const BaseModel = require('./BaseModel');

/**
 * 系统配置模型
 */
class SystemConfig extends BaseModel {
  constructor() {
    super('system_config');
  }

  /**
   * 设置配置项
   */
  setConfig(key, value, description = null) {
    const existing = this.getConfig(key);

    if (existing) {
      return this.updateWhere(
        { key },
        { value, description, updated_at: new Date().toISOString() }
      );
    } else {
      return this.insert({
        key,
        value,
        description,
        updated_at: new Date().toISOString(),
      });
    }
  }

  /**
   * 获取配置项
   */
  getConfig(key) {
    return this.findOneWhere({ key });
  }

  /**
   * 获取配置值
   */
  getConfigValue(key, defaultValue = null) {
    const config = this.getConfig(key);
    return config ? config.value : defaultValue;
  }

  /**
   * 删除配置项
   */
  deleteConfig(key) {
    return this.deleteWhere({ key });
  }

  /**
   * 获取所有配置
   */
  getAllConfigs() {
    return this.findAll('key ASC');
  }

  /**
   * 批量设置配置
   */
  batchSetConfigs(configs) {
    const db = this.getDb();

    const transaction = db.transaction(() => {
      Object.entries(configs).forEach(([key, value]) => {
        this.setConfig(key, value);
      });
    });

    transaction();
  }
}

/**
 * 会话管理模型
 */
class Session extends BaseModel {
  constructor() {
    super('sessions');
  }

  /**
   * 创建会话
   */
  createSession(sessionData) {
    const expiresAt = new Date();
    expiresAt.setHours(expiresAt.getHours() + 24); // 24小时后过期

    const data = {
      session_id: sessionData.session_id || this.generateSessionId(),
      password: sessionData.password,
      created_at: new Date().toISOString(),
      last_accessed_at: new Date().toISOString(),
      expires_at: sessionData.expires_at || expiresAt.toISOString(),
      is_active: 1,
    };

    this.insert(data);

    // 强制限制活跃会话数量为 10 条
    this.enforceSessionLimit(10);

    return data;
  }

  /**
   * 强制限制会话数量
   */
  enforceSessionLimit(limit = 10) {
    try {
      const db = this.getDb();
      // 删除除了最近访问的 limit 条之外的所有会话
      db.prepare(
        `
                DELETE FROM ${this.tableName}
                WHERE session_id NOT IN (
                    SELECT session_id FROM ${this.tableName}
                    ORDER BY last_accessed_at DESC
                    LIMIT ?
                )
            `
      ).run(limit);
    } catch (e) {
      console.error('❌ 限制会话数量失败:', e.message);
    }
  }

  /**
   * 生成会话 ID
   */
  generateSessionId() {
    return `sess_${Date.now()}_${Math.random().toString(36).substr(2, 16)}`;
  }

  /**
   * 获取会话
   */
  getSession(sessionId) {
    return this.findOneWhere({ session_id: sessionId });
  }

  /**
   * 验证会话
   */
  validateSession(sessionId) {
    const session = this.getSession(sessionId);

    if (!session) {
      return { valid: false, reason: 'session_not_found' };
    }

    if (!session.is_active) {
      return { valid: false, reason: 'session_inactive' };
    }

    const now = new Date();
    const expiresAt = new Date(session.expires_at);

    if (now > expiresAt) {
      // 会话已过期，标记为不活跃
      this.updateWhere({ session_id: sessionId }, { is_active: 0 });
      return { valid: false, reason: 'session_expired' };
    }

    // 更新最后访问时间
    this.updateLastAccessed(sessionId);

    return { valid: true, session };
  }

  /**
   * 更新最后访问时间
   */
  updateLastAccessed(sessionId) {
    return this.updateWhere(
      { session_id: sessionId },
      {
        last_accessed_at: new Date().toISOString(),
      }
    );
  }

  /**
   * 使会话失效
   */
  invalidateSession(sessionId) {
    return this.updateWhere({ session_id: sessionId }, { is_active: 0 });
  }

  /**
   * 清理过期或长期未使用的会话
   */
  cleanExpiredSessions() {
    const db = this.getDb();
    // 清理规则：
    // 1. 明确已过期的 (expires_at < now)
    // 2. 标记为不活跃且超过 7 天未访问的
    // 3. 活跃但超过 3 天未访问的（认为是僵尸会话）
    const stmt = db.prepare(`
            DELETE FROM ${this.tableName}
            WHERE expires_at < datetime('now')
                OR is_active = 0
                OR last_accessed_at < datetime('now', '-3 days')
        `);
    return stmt.run().changes;
  }

  /**
   * 获取活跃会话数
   */
  getActiveSessionCount() {
    return this.count({ is_active: 1 });
  }

  /**
   * 获取所有活跃会话
   */
  getActiveSessions() {
    const db = this.getDb();
    const stmt = db.prepare(`
            SELECT * FROM ${this.tableName}
            WHERE is_active = 1 AND expires_at > datetime('now')
            ORDER BY last_accessed_at DESC
        `);
    return stmt.all();
  }
}

/**
 * 用户设置模型
 */
class UserSettings extends BaseModel {
  constructor() {
    super('user_settings');
  }

  /**
   * 确保外观偏好字段存在
   */
  ensureAppearanceColumns() {
    const columns = [
      {
        name: 'theme_mode',
        sql: `ALTER TABLE ${this.tableName} ADD COLUMN theme_mode TEXT DEFAULT 'auto'`,
        fallback: 'auto',
      },
      {
        name: 'page_width_mode',
        sql: `ALTER TABLE ${this.tableName} ADD COLUMN page_width_mode TEXT DEFAULT 'standard'`,
        fallback: 'standard',
      },
      {
        name: 'sidebar_collapsed',
        sql: `ALTER TABLE ${this.tableName} ADD COLUMN sidebar_collapsed INTEGER DEFAULT 0`,
        fallback: 0,
      },
    ];

    columns.forEach(({ name, sql, fallback }) => {
      if (!this.hasColumn(name)) {
        try {
          this.getDb().prepare(sql).run();
        } catch (e) {
          console.warn(`Auto-migration for ${name} failed:`, e.message);
        }
      } else {
        this.getDb()
          .prepare(`UPDATE ${this.tableName} SET ${name} = ? WHERE ${name} IS NULL`)
          .run(fallback);
      }
    });
  }

  /**
   * 获取用户设置
   */
  getSettings() {
    this.ensureAppearanceColumns();

    let settings = this.findById(1);

    if (!settings) {
      // 如果不存在，创建默认设置
      settings = this.createDefaultSettings();
    }

    const defaults = {
      dns: true,
      openai: true,
      server: true,
    };
    const defaultOrder = ['dns', 'openai', 'server'];

    // 解析 JSON 字段并合并默认值
    if (settings.module_visibility) {
      const parsed = JSON.parse(settings.module_visibility);
      settings.module_visibility = { ...defaults, ...parsed };
      delete settings.module_visibility.antigravity;
    } else {
      settings.module_visibility = defaults;
    }

    if (settings.channel_enabled) {
      const parsed = JSON.parse(settings.channel_enabled);
      // 确保都有值
      settings.channel_enabled = {
        'gemini-cli': true,
        ...parsed,
      };
      delete settings.channel_enabled.antigravity;
    } else {
      settings.channel_enabled = {
        'gemini-cli': true,
      };
    }

    if (settings.module_order) {
      const parsed = JSON.parse(settings.module_order).filter(module => module !== 'antigravity');
      // 确保所有默认模块都在顺序列表中
      const missing = defaultOrder.filter(m => !parsed.includes(m));
      settings.module_order = [...parsed, ...missing];
    } else {
      settings.module_order = defaultOrder;
    }

    if (settings.channel_model_prefix) {
      try {
        settings.channel_model_prefix = JSON.parse(settings.channel_model_prefix);
      } catch (e) {
        settings.channel_model_prefix = { 'gemini-cli': '' };
      }
      delete settings.channel_model_prefix.antigravity;
    } else {
      settings.channel_model_prefix = { 'gemini-cli': '' };
    }

    if (!settings.load_balancing_strategy) {
      settings.load_balancing_strategy = 'random';
    }

    if (settings.vibration_enabled === undefined) {
      settings.vibration_enabled = 1;
    }

    if (settings.totp_settings) {
      try {
        settings.totp_settings = JSON.parse(settings.totp_settings);
      } catch (e) {
        settings.totp_settings = {};
      }
    } else {
      settings.totp_settings = {};
    }

    return settings;
  }

  /**
   * 创建默认设置
   */
  createDefaultSettings() {
    const defaultSettings = {
      id: 1,
      custom_css: '',
      theme_mode: 'auto',
      page_width_mode: 'standard',
      sidebar_collapsed: 0,
      module_visibility: JSON.stringify({
        dns: true,
        openai: true,
        server: true,
      }),
      channel_enabled: JSON.stringify({
        'gemini-cli': true,
      }),
      channel_model_prefix: JSON.stringify({
        'gemini-cli': '',
      }),
      module_order: JSON.stringify(['dns', 'openai', 'server']),
      updated_at: new Date().toISOString(),
    };

    this.insert(defaultSettings);
    return defaultSettings;
  }

  /**
   * 更新用户设置
   */
  updateSettings(updates) {
    this.ensureAppearanceColumns();

    // 自动迁移：确保新字段存在
    if (!this.hasColumn('channel_model_prefix')) {
      try {
        this.getDb()
          .prepare(`ALTER TABLE ${this.tableName} ADD COLUMN channel_model_prefix TEXT`)
          .run();
      } catch (e) {
        console.warn('Auto-migration for channel_model_prefix failed:', e.message);
      }
    }

    if (!this.hasColumn('vibration_enabled')) {
      try {
        this.getDb()
          .prepare(`ALTER TABLE ${this.tableName} ADD COLUMN vibration_enabled INTEGER DEFAULT 1`)
          .run();
      } catch (e) {
        console.warn('Auto-migration for vibration_enabled failed:', e.message);
      }
    }

    if (!this.hasColumn('totp_settings')) {
      try {
        this.getDb().prepare(`ALTER TABLE ${this.tableName} ADD COLUMN totp_settings TEXT`).run();
      } catch (e) {
        console.warn('Auto-migration for totp_settings failed:', e.message);
      }
    }

    if (!this.hasColumn('agent_download_url')) {
      try {
        this.getDb()
          .prepare(`ALTER TABLE ${this.tableName} ADD COLUMN agent_download_url TEXT`)
          .run();
      } catch (e) {
        console.warn('Auto-migration for agent_download_url failed:', e.message);
      }
    }

    if (!this.hasColumn('koyeb_refresh_interval')) {
      try {
        this.getDb()
          .prepare(
            `ALTER TABLE ${this.tableName} ADD COLUMN koyeb_refresh_interval INTEGER DEFAULT 30000`
          )
          .run();
      } catch (e) {
        console.warn('Auto-migration for koyeb_refresh_interval failed:', e.message);
      }
    }

    if (!this.hasColumn('fly_refresh_interval')) {
      try {
        this.getDb()
          .prepare(
            `ALTER TABLE ${this.tableName} ADD COLUMN fly_refresh_interval INTEGER DEFAULT 30000`
          )
          .run();
      } catch (e) {
        console.warn('Auto-migration for fly_refresh_interval failed:', e.message);
      }
    }

    if (!this.hasColumn('public_api_url')) {
      try {
        this.getDb()
          .prepare(`ALTER TABLE ${this.tableName} ADD COLUMN public_api_url TEXT`)
          .run();
      } catch (e) {
        console.warn('Auto-migration for public_api_url failed:', e.message);
      }
    }

    const data = { ...updates };
    Object.keys(data).forEach((key) => {
      if (data[key] === undefined) {
        delete data[key];
      }
    });

    // 处理 JSON 字段
    if (data.module_visibility && typeof data.module_visibility !== 'string') {
      data.module_visibility = JSON.stringify(data.module_visibility);
    }
    if (data.module_order && typeof data.module_order !== 'string') {
      data.module_order = JSON.stringify(data.module_order);
    }
    if (data.channel_enabled && typeof data.channel_enabled !== 'string') {
      data.channel_enabled = JSON.stringify(data.channel_enabled);
    }
    if (data.channel_model_prefix && typeof data.channel_model_prefix !== 'string') {
      data.channel_model_prefix = JSON.stringify(data.channel_model_prefix);
    }
    if (data.totp_settings && typeof data.totp_settings !== 'string') {
      data.totp_settings = JSON.stringify(data.totp_settings);
    }

    return this.update(1, data);
  }

  /**
   * 重置为默认设置
   */
  resetToDefault() {
    this.delete(1);
    return this.createDefaultSettings();
  }
}

/**
 * 操作日志模型
 */
const { asyncLocalStorage } = require('../../utils/logger');

class OperationLog extends BaseModel {
  constructor() {
    super('operation_logs');
  }

  /**
   * 记录操作 (审计日志)
   */
  logOperation(logData) {
    // 从异步上下文中获取信息
    const context = asyncLocalStorage.getStore() || {};

    const data = {
      operation_type: logData.operation_type,
      table_name: logData.table_name,
      record_id: logData.record_id || null,
      details: logData.details
        ? typeof logData.details === 'string'
          ? logData.details
          : JSON.stringify(logData.details)
        : null,
      ip_address: logData.ip_address || context.ip || null,
      user_agent: logData.user_agent || null,
      trace_id: context.traceId || null, // 关联 Trace ID
      created_at: new Date().toISOString(),
    };

    this.insert(data);
    return data;
  }

  /**
   * 获取操作日志
   */
  getLogs(tableName = null, limit = 100) {
    const db = this.getDb();

    if (tableName) {
      const stmt = db.prepare(`
                SELECT * FROM ${this.tableName}
                WHERE table_name = ?
                ORDER BY created_at DESC
                LIMIT ?
            `);
      return stmt.all(tableName, limit);
    }

    const stmt = db.prepare(`
            SELECT * FROM ${this.tableName}
            ORDER BY created_at DESC
            LIMIT ?
        `);
    return stmt.all(limit);
  }

  /**
   * 获取最近的操作
   */
  getRecentLogs(hours = 24, limit = 100) {
    const db = this.getDb();
    const stmt = db.prepare(`
            SELECT * FROM ${this.tableName}
            WHERE created_at >= datetime('now', '-${hours} hours')
            ORDER BY created_at DESC
            LIMIT ?
        `);
    return stmt.all(limit);
  }

  /**
   * 清理旧日志
   */
  cleanOldLogs(days = 30) {
    const db = this.getDb();
    const stmt = db.prepare(`
            DELETE FROM ${this.tableName}
            WHERE created_at < datetime('now', '-${days} days')
        `);
    return stmt.run().changes;
  }

  /**
   * 获取操作统计
   */
  getOperationStats(days = 7) {
    const db = this.getDb();
    const stmt = db.prepare(`
            SELECT
                operation_type,
                table_name,
                COUNT(*) as count
            FROM ${this.tableName}
            WHERE created_at >= datetime('now', '-${days} days')
            GROUP BY operation_type, table_name
            ORDER BY count DESC
        `);
    return stmt.all();
  }
}

/**
 * 登录尝试记录模型
 * 用于登录保护（防暴力破解）
 */
class LoginAttempt extends BaseModel {
  constructor() {
    super('login_attempts');
    this.ensureTable();
  }

  /**
   * 确保表存在
   */
  ensureTable() {
    try {
      const db = this.getDb();
      db.prepare(`
        CREATE TABLE IF NOT EXISTS login_attempts (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          ip_address TEXT NOT NULL,
          failed_count INTEGER DEFAULT 0,
          locked_until TEXT,
          last_attempt TEXT,
          created_at TEXT DEFAULT (datetime('now')),
          UNIQUE(ip_address)
        )
      `).run();
    } catch (e) {
      // 表可能已存在，忽略错误
    }
  }

  /**
   * 记录失败的登录尝试
   * @param {string} ip - IP 地址
   * @returns {Object} 包含是否锁定和剩余尝试次数的信息
   */
  recordFailedAttempt(ip) {
    const db = this.getDb();
    const now = new Date().toISOString();
    const maxAttempts = 5;
    const lockDurationMinutes = 15;

    // 获取或创建记录
    const record = this.findOneWhere({ ip_address: ip });

    if (!record) {
      // 首次失败
      db.prepare(`
        INSERT INTO login_attempts (ip_address, failed_count, last_attempt)
        VALUES (?, 1, ?)
      `).run(ip, now);

      return {
        locked: false,
        remainingAttempts: maxAttempts - 1,
        lockUntil: null,
      };
    }

    // 检查是否仍在锁定期
    if (record.locked_until && new Date(record.locked_until) > new Date()) {
      return {
        locked: true,
        remainingAttempts: 0,
        lockUntil: record.locked_until,
      };
    }

    // 如果锁定已过期，重置计数器
    if (record.locked_until && new Date(record.locked_until) <= new Date()) {
      db.prepare(`
        UPDATE login_attempts
        SET failed_count = 1, locked_until = NULL, last_attempt = ?
        WHERE ip_address = ?
      `).run(now, ip);

      return {
        locked: false,
        remainingAttempts: maxAttempts - 1,
        lockUntil: null,
      };
    }

    // 增加失败计数
    const newCount = record.failed_count + 1;

    if (newCount >= maxAttempts) {
      // 达到上限，锁定账户
      const lockUntil = new Date();
      lockUntil.setMinutes(lockUntil.getMinutes() + lockDurationMinutes);
      const lockUntilStr = lockUntil.toISOString();

      db.prepare(`
        UPDATE login_attempts
        SET failed_count = ?, locked_until = ?, last_attempt = ?
        WHERE ip_address = ?
      `).run(newCount, lockUntilStr, now, ip);

      return {
        locked: true,
        remainingAttempts: 0,
        lockUntil: lockUntilStr,
      };
    }

    // 更新失败计数
    db.prepare(`
      UPDATE login_attempts
      SET failed_count = ?, last_attempt = ?
      WHERE ip_address = ?
    `).run(newCount, now, ip);

    return {
      locked: false,
      remainingAttempts: maxAttempts - newCount,
      lockUntil: null,
    };
  }

  /**
   * 检查 IP 是否被锁定
   * @param {string} ip - IP 地址
   * @returns {Object} 锁定状态
   */
  isLocked(ip) {
    const record = this.findOneWhere({ ip_address: ip });

    if (!record || !record.locked_until) {
      return { locked: false };
    }

    const lockUntil = new Date(record.locked_until);
    if (lockUntil > new Date()) {
      return {
        locked: true,
        lockUntil: record.locked_until,
        remainingSeconds: Math.ceil((lockUntil - new Date()) / 1000),
      };
    }

    return { locked: false };
  }

  /**
   * 登录成功后重置尝试次数
   * @param {string} ip - IP 地址
   */
  resetAttempts(ip) {
    const db = this.getDb();
    db.prepare(`
      DELETE FROM login_attempts
      WHERE ip_address = ?
    `).run(ip);
  }

  /**
   * 清理过期的锁定记录
   */
  cleanExpiredLocks() {
    const db = this.getDb();
    const result = db.prepare(`
      DELETE FROM login_attempts
      WHERE locked_until IS NOT NULL AND locked_until < datetime('now')
    `).run();
    return result.changes;
  }
}

module.exports = {
  SystemConfig: new SystemConfig(),
  Session: new Session(),
  UserSettings: new UserSettings(),
  OperationLog: new OperationLog(),
  LoginAttempt: new LoginAttempt(),
};
