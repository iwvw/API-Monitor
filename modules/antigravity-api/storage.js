const dbService = require('../../src/db/database');
const { v4: uuidv4 } = require('uuid');

// 初始化数据库
dbService.initialize();

// 确保存储模型配置的表存在
try {
  const db = dbService.getDatabase();
  db.prepare(
    `
        CREATE TABLE IF NOT EXISTS antigravity_model_config (
            model_id TEXT PRIMARY KEY,
            enabled INTEGER DEFAULT 1,
            updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
        )
    `
  ).run();
} catch (e) {
  console.error('❌ 初始化 Antigravity 模型配置表失败:', e.message);
}

// 确保设置表存在
try {
  const db = dbService.getDatabase();
  db.prepare(
    `
        CREATE TABLE IF NOT EXISTS antigravity_settings (
            key TEXT PRIMARY KEY,
            value TEXT,
            updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
        )
    `
  ).run();
} catch (e) {
  console.error('❌ 初始化 Antigravity 设置表失败:', e.message);
}

/**
 * 获取所有 Antigravity 账号 (带统计)
 */
function getAccounts() {
  try {
    const db = dbService.getDatabase();
    return db
      .prepare(
        `
            SELECT 
                a.*,
                (SELECT project_id FROM antigravity_tokens t WHERE t.account_id = a.id LIMIT 1) as projectId,
                (SELECT COUNT(*) FROM antigravity_logs l WHERE l.account_id = a.id AND l.status_code = 200) as success_count,
                (SELECT COUNT(*) FROM antigravity_logs l WHERE l.account_id = a.id AND l.status_code != 200) as error_count
            FROM antigravity_accounts a
        `
      )
      .all();
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
        lastUsed: 'last_used',
      };

      if (map[key]) {
        fields.push(`${map[key]} = ?`);
        values.push(value === true ? 1 : value === false ? 0 : value);
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
    return db
      .prepare('SELECT * FROM antigravity_tokens WHERE account_id = ? AND enable = 1')
      .get(accountId);
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
    const existing = db
      .prepare('SELECT id FROM antigravity_tokens WHERE account_id = ?')
      .get(tokenData.accountId);

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
            INSERT INTO antigravity_logs (account_id, model, is_balanced, request_path, request_method, status_code, duration_ms, client_ip, user_agent, detail)
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
    console.error('❌ 记录调用日志失败:', e.message);
  }
}

/**
 * 获取最近日志（包含账号名称和模型）
 */
function getRecentLogs(limit = 100) {
  try {
    const db = dbService.getDatabase();
    const logs = db
      .prepare(
        `
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
            FROM antigravity_logs l
            LEFT JOIN antigravity_accounts a ON l.account_id = a.id
            ORDER BY l.created_at DESC 
            LIMIT ?
        `
      )
      .all(limit);

    // 如果 model 列为空，尝试从 detail 字段提取 (兼容旧数据)
    return logs.map(log => {
      let model = log.model;
      if (!model && log.detail) {
        try {
          const detail = JSON.parse(log.detail);
          model = detail.model || null;
        } catch (e) {}
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
    const row = db
      .prepare('SELECT enabled FROM antigravity_model_config WHERE model_id = ?')
      .get(modelId);
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
  db.prepare(
    `
        CREATE TABLE IF NOT EXISTS antigravity_model_redirects (
            source_model TEXT PRIMARY KEY,
            target_model TEXT NOT NULL,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP
        )
    `
  ).run();
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
    db.prepare(
      `
            INSERT INTO antigravity_model_redirects (source_model, target_model)
            VALUES (?, ?)
            ON CONFLICT(source_model) DO UPDATE SET target_model = excluded.target_model
        `
    ).run(sourceModel, targetModel);
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

// 确保模型检测历史表存在
try {
  const db = dbService.getDatabase();
  db.prepare(
    `
        CREATE TABLE IF NOT EXISTS antigravity_model_checks (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            model_id TEXT NOT NULL,
            status TEXT NOT NULL,
            error_message TEXT,
            check_time INTEGER NOT NULL,
            passed_accounts TEXT,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP
        )
    `
  ).run();
  // 创建索引以加速查询，并确保 (model_id, check_time) 唯一以便增量更新
  db.prepare(
    'CREATE UNIQUE INDEX IF NOT EXISTS idx_ag_model_checks_unique ON antigravity_model_checks(model_id, check_time)'
  ).run();
  db.prepare(
    'CREATE INDEX IF NOT EXISTS idx_ag_model_checks_time ON antigravity_model_checks(check_time)'
  ).run();
} catch (e) {
  console.error('❌ 初始化 Antigravity 模型检测表失败:', e.message);
}

/**
 * 记录模型检测结果
 */
function recordModelCheck(
  modelId,
  status,
  errorMessage = null,
  checkTime = null,
  passedAccounts = null
) {
  try {
    const db = dbService.getDatabase();
    const time = checkTime || Math.floor(Date.now() / 1000);
    db.prepare(
      `
            INSERT INTO antigravity_model_checks (model_id, status, error_message, check_time, passed_accounts)
            VALUES (?, ?, ?, ?, ?)
            ON CONFLICT(model_id, check_time) DO UPDATE SET
                status = excluded.status,
                error_message = excluded.error_message,
                passed_accounts = excluded.passed_accounts
        `
    ).run(modelId, status, errorMessage, time, passedAccounts);
    return true;
  } catch (e) {
    console.error('❌ 记录模型检测失败:', e.message);
    return false;
  }
}

/**
 * 获取模型检测历史
 */
function getModelCheckHistory() {
  try {
    const db = dbService.getDatabase();

    // 获取最近 10 次检测的时间戳
    const times = db
      .prepare(
        `
            SELECT DISTINCT check_time FROM antigravity_model_checks 
            ORDER BY check_time DESC LIMIT 10
        `
      )
      .all()
      .map(r => r.check_time);

    // 获取所有检测过的模型
    const models = db
      .prepare(
        `
            SELECT DISTINCT model_id FROM antigravity_model_checks 
            ORDER BY model_id
        `
      )
      .all()
      .map(r => r.model_id);

    if (times.length === 0) {
      return { models: [], times: [], matrix: {} };
    }

    // 获取这些时间范围内的检测记录
    const checks = db
      .prepare(
        `
            SELECT model_id, status, check_time, passed_accounts, error_message FROM antigravity_model_checks 
            WHERE check_time IN (${times.map(() => '?').join(',')})
        `
      )
      .all(...times);

    // 构建矩阵数据
    const matrix = {};
    models.forEach(model => {
      matrix[model] = {};
    });
    checks.forEach(check => {
      if (matrix[check.model_id]) {
        matrix[check.model_id][check.check_time] = {
          status: check.status,
          passedAccounts: check.passed_accounts || '',
          error_log: check.error_message || '',
        };
      }
    });

    return { models, times, matrix };
  } catch (e) {
    console.error('❌ 获取检测历史失败:', e.message);
    return { models: [], times: [], matrix: {} };
  }
}

/**
 * 清空模型检测历史
 */
function clearModelCheckHistory() {
  try {
    const db = dbService.getDatabase();
    db.prepare('DELETE FROM antigravity_model_checks').run();
    return true;
  } catch (e) {
    console.error('❌ 清空检测历史失败:', e.message);
    return false;
  }
}

/**
 * 获取统计信息
 */
function getStats() {
  try {
    const db = dbService.getDatabase();
    const stats = db
      .prepare(
        `
            SELECT 
                COUNT(*) as total_calls,
                SUM(CASE WHEN status_code = 200 THEN 1 ELSE 0 END) as success_calls,
                SUM(CASE WHEN status_code != 200 THEN 1 ELSE 0 END) as fail_calls
            FROM antigravity_logs
        `
      )
      .get();

    const accounts = getAccounts();
    return {
      total_calls: stats.total_calls || 0,
      success_calls: stats.success_calls || 0,
      fail_calls: stats.fail_calls || 0,
      accounts: {
        total: accounts.length,
        online: accounts.filter(a => a.status === 'online').length,
        enabled: accounts.filter(a => a.enable).length,
      },
    };
  } catch (e) {
    console.error('❌ 获取统计失败:', e.message);
    return {
      total_calls: 0,
      success_calls: 0,
      fail_calls: 0,
      accounts: { total: 0, online: 0, enabled: 0 },
    };
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
  updateModelStatus,
  getModelRedirects,
  addModelRedirect,
  removeModelRedirect,
  recordModelCheck,
  getModelCheckHistory,
  clearModelCheckHistory,
  getStats,
};
