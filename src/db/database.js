const Database = require('better-sqlite3');
const fs = require('fs');
const path = require('path');
const { createLogger } = require('../utils/logger');

const logger = createLogger('Database');

class DatabaseService {
  constructor() {
    this.db = null;
    this.initialized = false;
    const dataDir = process.env.DATA_DIR || path.join(__dirname, '../../data');
    const dbName = process.env.DB_NAME || 'data.db';
    this.dbPath = path.isAbsolute(dataDir)
      ? path.join(dataDir, dbName)
      : path.resolve(process.cwd(), dataDir, dbName);
    this.schemaPath = path.join(__dirname, 'schema.sql');
  }

  /**
   * 初始化数据库连接
   */
  initialize() {
    // 防止重复初始化
    if (this.initialized) {
      return this.db;
    }

    try {
      logger.start('初始化数据库连接');

      // 确保 data 目录存在
      const dataDir = path.dirname(this.dbPath);
      if (!fs.existsSync(dataDir)) {
        fs.mkdirSync(dataDir, { recursive: true });
        logger.info('创建数据目录: ' + dataDir);
      }

      // 创建或打开数据库
      this.db = new Database(this.dbPath, {
        verbose: process.env.NODE_ENV === 'development' ? null : null,
      });

      // 启用外键约束
      this.db.pragma('foreign_keys = ON');

      // 启用 WAL 模式 (提升并发性能)
      this.db.pragma('journal_mode = WAL');
      this.db.pragma('synchronous = NORMAL');
      this.db.pragma('wal_autocheckpoint = 1000'); // 每 1000 页自动 checkpoint

      // 执行数据库初始化脚本
      this.initializeSchema();

      this.initialized = true;
      logger.success('数据库初始化完成');

      return this.db;
    } catch (error) {
      logger.error('初始化数据库连接失败: ' + error.message);
      throw error;
    }
  }

  /**
   * 关闭数据库连接
   * 在关闭前执行分次 checkpoint 以确保数据合并到主文件并清理临时文件
   */
  close() {
    if (this.db) {
      try {
        logger.info('正在关闭数据库连接...');
        this.db.pragma('wal_checkpoint(TRUNCATE)');
        this.db.close();
        this.db = null;
        this.initialized = false;
        logger.success('数据库连接已安全关闭');
      } catch (error) {
        logger.error('关闭数据库时发生错误: ' + error.message);
      }
    }
  }

  /**
   * 执行数据库表结构初始化
   */
  initializeSchema() {
    try {
      // 1. 初始化核心 Schema
      const schema = fs.readFileSync(this.schemaPath, 'utf8');
      this.db.exec(schema);
      logger.debug('核心数据库表结构已同步');

      // 2. 初始化模块 Schema
      const modulesDir = path.join(__dirname, '../../modules');
      if (fs.existsSync(modulesDir)) {
        const modules = fs
          .readdirSync(modulesDir, { withFileTypes: true })
          .filter(dirent => dirent.isDirectory() && !dirent.name.startsWith('_'))
          .map(dirent => dirent.name);

        modules.forEach(moduleName => {
          const moduleSchemaPath = path.join(modulesDir, moduleName, 'schema.sql');
          if (fs.existsSync(moduleSchemaPath)) {
            try {
              const moduleSchema = fs.readFileSync(moduleSchemaPath, 'utf8');
              this.db.exec(moduleSchema);
              logger.debug(`模块数据库表结构已同步: ${moduleName}`);
            } catch (err) {
              logger.error(`模块 Schema 初始化失败 (${moduleName}):`, err.message);
              // 继续初始化其他模块
            }
          }
        });
      }

      // 3. 执行数据库迁移
      this.runMigrations();
    } catch (error) {
      logger.error('数据库表结构初始化失败', error.message);
      throw error;
    }
  }

  /**
   * 执行数据库迁移
   */
  runMigrations() {
    try {
      // 检查 server_credentials 表是否有 is_default 字段
      const columns = this.db.pragma('table_info(server_credentials)');
      const hasIsDefault = columns.some(col => col.name === 'is_default');

      if (!hasIsDefault) {
        logger.info('正在为 server_credentials 表添加 is_default 字段...');
        this.db.exec('ALTER TABLE server_credentials ADD COLUMN is_default INTEGER DEFAULT 0');
        logger.success('is_default 字段添加成功');
      }

      // Antigravity 迁移: 检查 antigravity_logs 表是否有 detail 字段
      try {
        const agLogsColumns = this.db.pragma('table_info(antigravity_logs)');
        if (agLogsColumns.length > 0) {
          const hasDetail = agLogsColumns.some(col => col.name === 'detail');
          if (!hasDetail) {
            logger.info('正在为 antigravity_logs 表添加 detail 字段...');
            this.db.exec('ALTER TABLE antigravity_logs ADD COLUMN detail TEXT');
            logger.success('antigravity_logs.detail 字段添加成功');
          }
        }
      } catch (err) {
        logger.error('Antigravity 额外字段迁移失败:', err.message);
      }

      // Gemini CLI 迁移: 检查 gemini_cli_accounts 表是否有 project_id 字段
      try {
        const gcliColumns = this.db.pragma('table_info(gemini_cli_accounts)');
        if (gcliColumns.length > 0) {
          const hasProjectId = gcliColumns.some(col => col.name === 'project_id');
          if (!hasProjectId) {
            logger.info('正在为 gemini_cli_accounts 表添加 project_id 字段...');
            this.db.exec('ALTER TABLE gemini_cli_accounts ADD COLUMN project_id TEXT');
            logger.success('gemini_cli_accounts.project_id 字段添加成功');
          }

          // 添加 cloudaicompanion_project_id 字段 (用于缓存 loadCodeAssist 返回的项目 ID)
          const hasCloudaicompanion = gcliColumns.some(
            col => col.name === 'cloudaicompanion_project_id'
          );
          if (!hasCloudaicompanion) {
            logger.info('正在为 gemini_cli_accounts 表添加 cloudaicompanion_project_id 字段...');
            this.db.exec(
              'ALTER TABLE gemini_cli_accounts ADD COLUMN cloudaicompanion_project_id TEXT'
            );
            logger.success('gemini_cli_accounts.cloudaicompanion_project_id 字段添加成功');
          }
        }
      } catch (err) {
        logger.error('Gemini CLI 额外字段迁移失败:', err.message);
      }

      // User Settings 迁移: 检查 user_settings 表是否有 channel_enabled 字段
      try {
        const settingsColumns = this.db.pragma('table_info(user_settings)');
        if (settingsColumns.length > 0) {
          const hasChannelEnabled = settingsColumns.some(col => col.name === 'channel_enabled');
          if (!hasChannelEnabled) {
            logger.info('正在为 user_settings 表添加 channel_enabled 字段...');
            this.db.exec('ALTER TABLE user_settings ADD COLUMN channel_enabled TEXT');
            logger.success('user_settings.channel_enabled 字段添加成功');
          }

          const hasLoadStrategy = settingsColumns.some(
            col => col.name === 'load_balancing_strategy'
          );
          if (!hasLoadStrategy) {
            logger.info('正在为 user_settings 表添加 load_balancing_strategy 字段...');
            this.db.exec(
              "ALTER TABLE user_settings ADD COLUMN load_balancing_strategy TEXT DEFAULT 'random'"
            );
            logger.success('user_settings.load_balancing_strategy 字段添加成功');
          }

          const hasIpDisplayMode = settingsColumns.some(
            col => col.name === 'server_ip_display_mode'
          );
          if (!hasIpDisplayMode) {
            logger.info('正在为 user_settings 表添加 server_ip_display_mode 字段...');
            this.db.exec(
              "ALTER TABLE user_settings ADD COLUMN server_ip_display_mode TEXT DEFAULT 'normal'"
            );
            logger.success('user_settings.server_ip_display_mode 字段添加成功');
          }

          const hasMainTabsLayout = settingsColumns.some(col => col.name === 'main_tabs_layout');
          if (!hasMainTabsLayout) {
            logger.info('正在为 user_settings 表添加 main_tabs_layout 字段...');
            this.db.exec(
              "ALTER TABLE user_settings ADD COLUMN main_tabs_layout TEXT DEFAULT 'top'"
            );
            logger.success('user_settings.main_tabs_layout 字段添加成功');
          }

          const hasChannelModelPrefix = settingsColumns.some(
            col => col.name === 'channel_model_prefix'
          );
          if (!hasChannelModelPrefix) {
            logger.info('正在为 user_settings 表添加 channel_model_prefix 字段...');
            this.db.exec('ALTER TABLE user_settings ADD COLUMN channel_model_prefix TEXT');
            logger.success('user_settings.channel_model_prefix 字段添加成功');
          }
        }
      } catch (err) {
        logger.error('User Settings 额外字段迁移失败:', err.message);
      }

      // Antigravity Logs 迁移: 添加 model 字段
      try {
        const agLogColumns = this.db.pragma('table_info(antigravity_logs)');
        if (agLogColumns.length > 0) {
          const hasModel = agLogColumns.some(col => col.name === 'model');
          if (!hasModel) {
            logger.info('正在为 antigravity_logs 表添加 model 字段...');
            this.db.exec('ALTER TABLE antigravity_logs ADD COLUMN model TEXT');
            logger.success('antigravity_logs.model 字段添加成功');
          }

          const hasBalanced = agLogColumns.some(col => col.name === 'is_balanced');
          if (!hasBalanced) {
            logger.info('正在为 antigravity_logs 表添加 is_balanced 字段...');
            this.db.exec('ALTER TABLE antigravity_logs ADD COLUMN is_balanced INTEGER DEFAULT 0');
            logger.success('antigravity_logs.is_balanced 字段添加成功');
          }
        }
      } catch (err) {
        logger.error('Antigravity Logs 迁移失败:', err.message);
      }

      // Gemini CLI Logs 迁移: 添加 model 字段
      try {
        const gcliLogColumns = this.db.pragma('table_info(gemini_cli_logs)');
        if (gcliLogColumns.length > 0) {
          const hasModel = gcliLogColumns.some(col => col.name === 'model');
          if (!hasModel) {
            logger.info('正在为 gemini_cli_logs 表添加 model 字段...');
            this.db.exec('ALTER TABLE gemini_cli_logs ADD COLUMN model TEXT');
            logger.success('gemini_cli_logs.model 字段添加成功');
          }

          const hasBalanced = gcliLogColumns.some(col => col.name === 'is_balanced');
          if (!hasBalanced) {
            logger.info('正在为 gemini_cli_logs 表添加 is_balanced 字段...');
            this.db.exec('ALTER TABLE gemini_cli_logs ADD COLUMN is_balanced INTEGER DEFAULT 0');
            logger.success('gemini_cli_logs.is_balanced 字段添加成功');
          }
        }
      } catch (err) {
        logger.error('Gemini CLI Logs 迁移失败:', err.message);
      }

      // Operation Logs 迁移: 检查 operation_logs 表是否有 trace_id 字段
      try {
        const logColumns = this.db.pragma('table_info(operation_logs)');
        if (logColumns.length > 0) {
          const hasTraceId = logColumns.some(col => col.name === 'trace_id');
          if (!hasTraceId) {
            logger.info('正在为 operation_logs 表添加 trace_id 字段...');
            this.db.exec('ALTER TABLE operation_logs ADD COLUMN trace_id TEXT');
            logger.success('operation_logs.trace_id 字段添加成功');
          }
        }
      } catch (err) {
        logger.error('Operation Logs 额外字段迁移失败:', err.message);
      }

      // Server Accounts 迁移: 添加 monitor_mode 字段
      try {
        const serverColumns = this.db.pragma('table_info(server_accounts)');
        if (serverColumns.length > 0) {
          const hasMonitorMode = serverColumns.some(col => col.name === 'monitor_mode');
          if (!hasMonitorMode) {
            logger.info('正在为 server_accounts 表添加 monitor_mode 字段...');
            this.db.exec("ALTER TABLE server_accounts ADD COLUMN monitor_mode TEXT DEFAULT 'ssh'");
            logger.success('server_accounts.monitor_mode 字段添加成功');
          }
        }
      } catch (err) {
        logger.error('Server Accounts monitor_mode 迁移失败:', err.message);
      }

      // Server Metrics History 迁移: 添加 platform 字段
      try {
        const metricsColumns = this.db.pragma('table_info(server_metrics_history)');
        if (metricsColumns.length > 0) {
          const hasPlatform = metricsColumns.some(col => col.name === 'platform');
          if (!hasPlatform) {
            logger.info('正在为 server_metrics_history 表添加 platform 字段...');
            this.db.exec('ALTER TABLE server_metrics_history ADD COLUMN platform TEXT');
            logger.success('server_metrics_history.platform 字段添加成功');
          }
        }
      } catch (err) {
        logger.error('Server Metrics History platform 迁移失败:', err.message);
      }

      // Chat Sessions 迁移: 添加 endpoint_id 和 persona_id 字段
      try {
        const chatColumns = this.db.pragma('table_info(chat_sessions)');
        if (chatColumns.length > 0) {
          const hasEndpointId = chatColumns.some(col => col.name === 'endpoint_id');
          if (!hasEndpointId) {
            logger.info('正在为 chat_sessions 表添加 endpoint_id 字段...');
            this.db.exec('ALTER TABLE chat_sessions ADD COLUMN endpoint_id TEXT');
            logger.success('chat_sessions.endpoint_id 字段添加成功');
          }
          const hasPersonaId = chatColumns.some(col => col.name === 'persona_id');
          if (!hasPersonaId) {
            logger.info('正在为 chat_sessions 表添加 persona_id 字段...');
            this.db.exec('ALTER TABLE chat_sessions ADD COLUMN persona_id INTEGER');
            logger.success('chat_sessions.persona_id 字段添加成功');
          }
        }
      } catch (err) {
        logger.error('Chat Sessions 字段迁移失败:', err.message);
      }

      // Persona 迁移: 创建 chat_personas 表
      try {
        const personaTables = this.db.prepare("SELECT name FROM sqlite_master WHERE type='table' AND name='chat_personas'").all();
        if (personaTables.length === 0) {
          logger.info('正在创建 chat_personas 表...');
          this.db.exec(`
            CREATE TABLE IF NOT EXISTS chat_personas (
              id INTEGER PRIMARY KEY AUTOINCREMENT,
              name TEXT NOT NULL,
              system_prompt TEXT NOT NULL,
              icon TEXT DEFAULT 'fa-robot',
              is_default INTEGER DEFAULT 0,
              created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
              updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
            )
          `);
          // 插入默认人设
          this.db.prepare('INSERT INTO chat_personas (name, system_prompt, icon, is_default) VALUES (?, ?, ?, ?)').run('默认助手', '你是一个有用的 AI 助手。', 'fa-robot', 1);
          logger.success('chat_personas 表创建成功并初始化默认人设');
        }
      } catch (err) {
        logger.error('Persona 表迁移失败:', err.message);
      }

      // Music Settings 迁移: 创建 music_settings 表存储 Cookie
      try {
        const musicTables = this.db
          .prepare(
            `
                    SELECT name FROM sqlite_master WHERE type='table' AND name='music_settings'
                `
          )
          .all();

        if (musicTables.length === 0) {
          logger.info('正在创建 music_settings 表...');
          this.db.exec(`
                        CREATE TABLE IF NOT EXISTS music_settings (
                            id INTEGER PRIMARY KEY AUTOINCREMENT,
                            key TEXT UNIQUE NOT NULL,
                            value TEXT,
                            updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
                        )
                    `);
          logger.success('music_settings 表创建成功');
        }
      } catch (err) {
        logger.error('Music Settings 表创建失败:', err.message);
      }

      // AI Draw 迁移: 为 ai_draw_projects 表添加 provider_id 字段
      try {
        const drawProjectsColumns = this.db.pragma('table_info(ai_draw_projects)');
        if (drawProjectsColumns.length > 0) {
          const hasProviderId = drawProjectsColumns.some(col => col.name === 'provider_id');
          if (!hasProviderId) {
            logger.info('正在为 ai_draw_projects 表添加 provider_id 字段...');
            this.db.exec('ALTER TABLE ai_draw_projects ADD COLUMN provider_id TEXT');
            logger.success('ai_draw_projects.provider_id 字段添加成功');
          }
        }
      } catch (err) {
        logger.error('AI Draw 迁移失败:', err.message);
      }
    } catch (error) {
      logger.error('数据库迁移失败', error.message);
      // 不抛出错误，避免影响应用启动
    }
  }

  /**
   * 获取数据库实例
   */
  getDatabase() {
    if (!this.db) {
      this.initialize();
    }
    return this.db;
  }

  /**
   * 代理方法：直接调用 db.prepare()
   * 允许模块使用 db.prepare() 语法
   */
  prepare(sql) {
    return this.getDatabase().prepare(sql);
  }

  /**
   * 代理方法：直接调用 db.exec()
   */
  exec(sql) {
    return this.getDatabase().exec(sql);
  }

  /**
   * 代理方法：直接调用 db.pragma()
   */
  pragma(sql) {
    return this.getDatabase().pragma(sql);
  }

  /**
   * 关闭数据库连接
   */
  close() {
    if (this.db) {
      try {
        // 关闭前确保 WAL 内容已合并
        this.db.pragma('wal_checkpoint(TRUNCATE)');
      } catch (e) {
        logger.warn('WAL checkpoint failed during close:', e.message);
      }
      this.db.close();
      this.db = null;
      this.initialized = false;
      logger.info('数据库连接已关闭');
    }
  }

  /**
   * 执行事务
   * @param {Function} callback - 事务回调函数
   */
  transaction(callback) {
    const db = this.getDatabase();
    const transaction = db.transaction(callback);
    return transaction;
  }

  /**
   * 备份数据库
   * @param {string} backupPath - 备份文件路径
   */
  async backup(backupPath) {
    try {
      logger.info('开始执行数据库备份...');
      const db = this.getDatabase();

      // 在备份前强制执行 Checkpoint，确保 WAL 中的数据全部写入主数据库文件
      // 这能解决某些情况下导出数据丢失近期修改的问题
      this.db.pragma('wal_checkpoint(TRUNCATE)');

      // 使用 better-sqlite3 原生备份 API
      // 这会自动处理 WAL 合并和一致性，比直接复制文件更安全
      await db.backup(backupPath);

      logger.success('数据库备份完成: ' + backupPath);
      return backupPath;
    } catch (error) {
      logger.error('数据库备份失败', error.message);
      throw error;
    }
  }

  /**
   * 获取数据库统计信息
   */
  getStats() {
    const db = this.getDatabase();

    const tables = db
      .prepare(
        `
            SELECT name FROM sqlite_master
            WHERE type='table' AND name NOT LIKE 'sqlite_%'
        `
      )
      .all();

    const stats = {};
    tables.forEach(({ name }) => {
      const count = db.prepare(`SELECT COUNT(*) as count FROM ${name}`).get();
      stats[name] = count.count;
    });

    return {
      dbPath: this.dbPath,
      dbSize: fs.statSync(this.dbPath).size,
      tables: stats,
    };
  }

  /**
   * 清空所有表数据（保留表结构）
   */
  clearAllData() {
    const db = this.getDatabase();

    const tables = db
      .prepare(
        `
            SELECT name FROM sqlite_master
            WHERE type='table' AND name NOT LIKE 'sqlite_%'
        `
      )
      .all();

    const transaction = db.transaction(() => {
      tables.forEach(({ name }) => {
        db.prepare(`DELETE FROM ${name}`).run();
      });
    });

    transaction();
    logger.warn('所有表数据已清空');
  }

  /**
   * 执行数据库 VACUUM (压缩/整理)
   * 包含完整的 WAL checkpoint 以确保物理文件大小能正确收缩
   */
  vacuum() {
    try {
      const db = this.getDatabase();
      const beforeSize = fs.statSync(this.dbPath).size;

      logger.info(`开始执行数据库 VACUUM... (当前大小: ${(beforeSize / 1024 / 1024).toFixed(2)}MB)`);

      // 1. 先执行 TRUNCATE 模式的 WAL checkpoint
      // 这会将 WAL 文件中的所有数据写入主数据库文件，并清空 WAL 文件
      try {
        const checkpointResult = db.pragma('wal_checkpoint(TRUNCATE)');
        logger.debug('WAL Checkpoint 结果:', checkpointResult);
      } catch (e) {
        logger.warn('WAL Checkpoint 失败:', e.message);
      }

      // 2. 执行 VACUUM 压缩数据库
      // VACUUM 会重建整个数据库文件，释放未使用的页面
      db.exec('VACUUM');

      // 3. 再次执行 checkpoint 确保干净
      try {
        db.pragma('wal_checkpoint(TRUNCATE)');
      } catch (e) {
        // 忽略
      }

      const afterSize = fs.statSync(this.dbPath).size;
      const savedMB = ((beforeSize - afterSize) / 1024 / 1024).toFixed(2);

      logger.success(`数据库 VACUUM 完成: ${(beforeSize / 1024 / 1024).toFixed(2)}MB -> ${(afterSize / 1024 / 1024).toFixed(2)}MB (释放 ${savedMB}MB)`);

      return {
        beforeSizeMB: (beforeSize / 1024 / 1024).toFixed(2),
        afterSizeMB: (afterSize / 1024 / 1024).toFixed(2),
        savedMB: savedMB
      };
    } catch (error) {
      logger.error('数据库 VACUUM 失败', error.message);
      throw error;
    }
  }

  /**
   * 清理所有日志表数据
   */
  clearLogs() {
    try {
      const db = this.getDatabase();
      logger.info('开始清理日志数据...');

      // 查找所有以 _logs 或 _history 结尾的表
      const tables = db
        .prepare(
          `
                SELECT name FROM sqlite_master 
                WHERE type='table' AND (name LIKE '%_logs' OR name LIKE '%_history')
            `
        )
        .all();

      if (tables.length === 0) {
        logger.info('未发现日志或历史表');
        return 0;
      }

      let deletedCount = 0;
      const transaction = db.transaction(() => {
        tables.forEach(({ name }) => {
          const result = db.prepare(`DELETE FROM ${name}`).run();
          logger.debug(`已清理表 ${name}: ${result.changes} 条记录`);
          deletedCount += result.changes;
        });
      });

      transaction();

      logger.success(`日志清理完成，共清理 ${deletedCount} 条记录`);
      return deletedCount;
    } catch (error) {
      logger.error('日志清理失败', error.message);
      throw error;
    }
  }

  /**
   * 强制执行日志保留策略
   * @param {Object} limits - 限制配置
   * @param {number} limits.days - 保留天数 (0=不限制)
   * @param {number} limits.count - 单表最大记录数 (0=不限制)
   * @param {number} limits.dbSizeMB - 数据库最大大小MB (0=不限制)
   */
  enforceLogLimits(limits) {
    try {
      const db = this.getDatabase();
      const { days, count, dbSizeMB } = limits;

      if (!days && !count && !dbSizeMB) {
        return { deleted: 0, reason: 'no_limits' };
      }

      logger.info('开始执行日志保留策略检查...', limits);
      let totalDeleted = 0;

      // 查找所有日志或历史记录表
      const tables = db
        .prepare(
          `
                SELECT name FROM sqlite_master 
                WHERE type='table' AND (name LIKE '%_logs' OR name LIKE '%_history')
            `
        )
        .all();

      if (tables.length === 0) return { deleted: 0 };

      const transaction = db.transaction(() => {
        tables.forEach(({ name }) => {
          // Determine timestamp column name
          const columns = db.pragma(`table_info(${name})`);
          const timeCol = columns.find(c =>
            ['created_at', 'checked_at', 'timestamp', 'recorded_at', 'start_time'].includes(c.name)
          )?.name;

          if (!timeCol) {
            logger.warn(`跳过表 ${name}: 未找到时间戳字段`);
            return;
          }

          // 1. 按天数清理
          if (days > 0) {
            const result = db
              .prepare(
                `
                            DELETE FROM ${name} 
                            WHERE ${timeCol} < datetime('now', '-${days} days')
                        `
              )
              .run();
            if (result.changes > 0) {
              logger.debug(`[${name}] 清理过期日志(${days}天): ${result.changes}条`);
              totalDeleted += result.changes;
            }
          }

          // 2. 按数量清理 (保留最新的 N 条)
          if (count > 0) {
            // SQLite DELETE limit 语法比较特殊，通常用 subquery
            const result = db
              .prepare(
                `
                            DELETE FROM ${name} 
                            WHERE rowid NOT IN (
                                SELECT rowid FROM ${name} 
                                ORDER BY ${timeCol} DESC 
                                LIMIT ?
                            )
                        `
              )
              .run(count);

            if (result.changes > 0) {
              logger.debug(`[${name}] 清理超量日志(保留${count}条): ${result.changes}条`);
              totalDeleted += result.changes;
            }
          }
        });
      });

      transaction();

      // 3. 按数据库大小清理 - 如果超出限制，自动删除最老的数据直到低于限制
      if (dbSizeMB > 0) {
        let currentStats = fs.statSync(this.dbPath);
        let currentSizeMB = currentStats.size / (1024 * 1024);

        if (currentSizeMB > dbSizeMB) {
          logger.warn(
            `数据库大小 (${currentSizeMB.toFixed(2)}MB) 超过限制 (${dbSizeMB}MB)，开始自动清理旧数据...`
          );

          // 最多尝试 10 轮清理，防止无限循环
          let cleanupRounds = 0;
          const MAX_CLEANUP_ROUNDS = 10;

          while (currentSizeMB > dbSizeMB && cleanupRounds < MAX_CLEANUP_ROUNDS) {
            cleanupRounds++;
            let roundDeleted = 0;

            // 遍历所有日志表，删除每个表最老的 20% 记录
            tables.forEach(({ name }) => {
              const columns = db.pragma(`table_info(${name})`);
              const timeCol = columns.find(c =>
                ['created_at', 'checked_at', 'timestamp', 'recorded_at', 'start_time'].includes(c.name)
              )?.name;

              if (!timeCol) return;

              // 获取表记录总数
              const countResult = db.prepare(`SELECT COUNT(*) as cnt FROM ${name}`).get();
              const tableCount = countResult.cnt;

              if (tableCount > 10) {
                // 至少保留 10 条记录
                // 删除最老的 20% 记录 (至少删除 1 条)
                const deleteCount = Math.max(1, Math.floor(tableCount * 0.2));

                const deleteResult = db
                  .prepare(
                    `
                    DELETE FROM ${name} 
                    WHERE rowid IN (
                      SELECT rowid FROM ${name} 
                      ORDER BY ${timeCol} ASC 
                      LIMIT ?
                    )
                  `
                  )
                  .run(deleteCount);

                if (deleteResult.changes > 0) {
                  logger.debug(
                    `[轮次${cleanupRounds}] [${name}] 删除最老 ${deleteResult.changes} 条记录`
                  );
                  roundDeleted += deleteResult.changes;
                  totalDeleted += deleteResult.changes;
                }
              }
            });

            // 如果这一轮没有删除任何数据，停止循环
            if (roundDeleted === 0) {
              logger.info('没有更多可清理的日志数据');
              break;
            }

            // 执行 VACUUM 回收空间
            db.exec('VACUUM');

            // 重新检查大小
            currentStats = fs.statSync(this.dbPath);
            currentSizeMB = currentStats.size / (1024 * 1024);
            logger.info(
              `[轮次${cleanupRounds}] 清理 ${roundDeleted} 条，VACUUM 后大小: ${currentSizeMB.toFixed(2)}MB`
            );
          }

          if (currentSizeMB <= dbSizeMB) {
            logger.success(
              `数据库大小已降至 ${currentSizeMB.toFixed(2)}MB，低于限制 ${dbSizeMB}MB`
            );
          } else {
            logger.warn(
              `经过 ${cleanupRounds} 轮清理，数据库大小仍为 ${currentSizeMB.toFixed(2)}MB，可能存在非日志数据占用`
            );
          }
        }
      }

      return { deleted: totalDeleted };
    } catch (error) {
      logger.error('执行日志保留策略失败', error.message);
      throw error;
    }
  }
}

// 导出单例实例
const dbService = new DatabaseService();
module.exports = dbService;
