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
              // 特殊处理 uptime-api 不兼容的旧表结构冲突与自动数据迁移
              if (moduleName === 'uptime-api') {
                const tableInfo = this.db.pragma('table_info(uptime_monitors)');
                if (tableInfo.length > 0) {
                  const idCol = tableInfo.find(c => c.name === 'id');
                  const hasConflictingSchema = idCol && idCol.type === 'TEXT';
                  
                  if (hasConflictingSchema) {
                    logger.warn('检测到不兼容的旧版 uptime_monitors 表结构，正在进行重命名备份...');
                    const timestamp = Date.now();
                    const backupMonitorsTable = `uptime_monitors_backup_${timestamp}`;
                    const backupHeartbeatsTable = `uptime_heartbeats_backup_${timestamp}`;
                    
                    try { this.db.exec(`ALTER TABLE uptime_monitors RENAME TO ${backupMonitorsTable}`); } catch (e) { logger.error('重命名 uptime_monitors 失败:', e.message); }
                    try { this.db.exec(`ALTER TABLE uptime_heartbeats RENAME TO ${backupHeartbeatsTable}`); } catch (e) { logger.error('重命名 uptime_heartbeats 失败:', e.message); }
                    try { this.db.exec(`ALTER TABLE uptime_incidents RENAME TO uptime_incidents_backup_${timestamp}`); } catch (e) { }
                    logger.success(`不兼容的旧版 Uptime 表已成功备份为 *_backup_${timestamp}`);
                  }
                }
                
                // 执行标准 Schema 以创建正确的表结构
                if (!this.db.prepare("SELECT name FROM sqlite_master WHERE type='table' AND name = 'uptime_monitors'").get()) {
                  const moduleSchema = fs.readFileSync(moduleSchemaPath, 'utf8');
                  this.db.exec(moduleSchema);
                  logger.debug(`模块数据库表结构已同步: ${moduleName}`);
                }

                // 检测是否有未迁移的旧备份表并执行迁移 (过滤掉已迁移过的 _migrated 备份表)
                const backupTable = this.db.prepare("SELECT name FROM sqlite_master WHERE type='table' AND name LIKE 'uptime_monitors_backup_%' AND name NOT LIKE '%_migrated' ORDER BY name DESC LIMIT 1").get();
                if (backupTable) {
                  const backupMonitorsTable = backupTable.name;
                  const timestamp = backupMonitorsTable.replace('uptime_monitors_backup_', '');
                  const backupHeartbeatsTable = `uptime_heartbeats_backup_${timestamp}`;
                  
                  logger.info(`检测到尚未迁移的备份表 ${backupMonitorsTable}，开始自动数据迁移...`);
                  this.migrateUptimeData(backupMonitorsTable, backupHeartbeatsTable);
                }
              }

              if (moduleName !== 'uptime-api') {
                const moduleSchema = fs.readFileSync(moduleSchemaPath, 'utf8');
                this.db.exec(moduleSchema);
                logger.debug(`模块数据库表结构已同步: ${moduleName}`);
              }
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
   * 从旧备份表迁移 Uptime 监控数据到新表结构
   */
  migrateUptimeData(backupMonitorsTable, backupHeartbeatsTable) {
    try {
      const oldMonitors = this.db.prepare(`SELECT * FROM ${backupMonitorsTable}`).all();
      if (oldMonitors.length === 0) return;

      // 使用 Set/Map 对旧版备份表数据进行唯一键查重
      const uniqueMonitors = [];
      const seen = new Map(); // uniqueKey -> primaryOm
      const duplicateOldIds = new Map(); // duplicateOldId -> primaryOldId

      for (const om of oldMonitors) {
        // 映射类型相关字段以进行精确对比
        let hostname = null;
        let url = om.url;
        if (om.type === 'ping' || om.type === 'tcp') {
          hostname = om.url;
          url = null;
        }

        const key = `${om.name}|${om.type || 'http'}|${url || ''}|${hostname || ''}|${om.port || 0}`;
        if (!seen.has(key)) {
          seen.set(key, om);
          uniqueMonitors.push(om);
        } else {
          const primaryOm = seen.get(key);
          duplicateOldIds.set(om.id, primaryOm.id);
        }
      }

      logger.info(`正在从旧表迁移并去重 ${oldMonitors.length} 个监控项，其中非重复监控项有 ${uniqueMonitors.length} 个...`);

      const insertMonitor = this.db.prepare(`
        INSERT INTO uptime_monitors (name, type, url, hostname, port, interval, timeout, confirm_count, active, method, headers, body, ignore_tls, accepted_status_codes, tags, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
      `);

      const idMapping = {}; // old_id (TEXT) -> new_id (INTEGER)

      const tx = this.db.transaction(() => {
        for (const om of uniqueMonitors) {
          // Map type-specific fields
          let hostname = null;
          let url = om.url;
          if (om.type === 'ping' || om.type === 'tcp') {
            hostname = om.url;
            url = null;
          }

          // Map confirm_count (old retries)
          const confirmCount = om.retries !== undefined ? om.retries : 3;

          // Accepted status codes mapping
          let acceptedStatusCodes = null;
          if (om.accepted_statuscodes) {
            try {
              const parsed = JSON.parse(om.accepted_statuscodes);
              acceptedStatusCodes = Array.isArray(parsed) ? parsed.join(',') : String(parsed);
            } catch (e) {
              acceptedStatusCodes = om.accepted_statuscodes;
            }
          }

          const res = insertMonitor.run(
            om.name,
            om.type || 'http',
            url,
            hostname,
            om.port,
            om.interval || 60,
            om.timeout || 30,
            confirmCount,
            om.active !== undefined ? om.active : 1,
            om.method || 'GET',
            om.headers,
            om.body,
            om.ignore_tls || 0,
            acceptedStatusCodes,
            om.tags || '[]',
            om.created_at || new Date().toISOString(),
            om.updated_at || new Date().toISOString()
          );

          idMapping[om.id] = res.lastInsertRowid;
        }
      });
      tx();

      // 将重复的旧 ID 映射关系也填充为对应 Primary ID 的新数据库 ID，使对应心跳与 incident 数据无损合并迁移
      for (const [dupId, primaryId] of duplicateOldIds.entries()) {
        idMapping[dupId] = idMapping[primaryId];
      }

      logger.success(`监控项迁移与合并完成: ${uniqueMonitors.length} 个非重复监控项已迁移 (去重合并了 ${duplicateOldIds.size} 个重复监控项)`);

      // Migrate heartbeats if they exist
      try {
        const hasBackupHb = this.db.prepare("SELECT name FROM sqlite_master WHERE type='table' AND name = ?").get(backupHeartbeatsTable);
        if (hasBackupHb) {
          const oldHeartbeats = this.db.prepare(`SELECT * FROM ${backupHeartbeatsTable}`).all();
          if (oldHeartbeats.length > 0) {
            logger.info(`正在迁移 ${oldHeartbeats.length} 条心跳记录...`);
            const insertHeartbeat = this.db.prepare(`
              INSERT INTO uptime_heartbeats (monitor_id, status, ping, msg, created_at)
              VALUES (?, ?, ?, ?, ?)
            `);

            const hbTx = this.db.transaction(() => {
              for (const oh of oldHeartbeats) {
                const newMonitorId = idMapping[oh.monitor_id];
                if (newMonitorId) {
                  insertHeartbeat.run(
                    newMonitorId,
                    oh.status,
                    oh.ping || 0,
                    oh.msg || '',
                    oh.time || oh.created_at || new Date().toISOString()
                  );
                }
              }
            });
            hbTx();
          }
        }
      } catch (e) {
        logger.warn('心跳记录迁移失败:', e.message);
      }

      // Rename backup tables to mark them as migrated, preventing repeat migrations
      try {
        this.db.exec(`ALTER TABLE ${backupMonitorsTable} RENAME TO ${backupMonitorsTable}_migrated`);
        logger.info(`备份表 ${backupMonitorsTable} 已成功标记为已迁移`);
      } catch (e) {
        logger.error(`重命名备份表 ${backupMonitorsTable} 失败:`, e.message);
      }
      try {
        const hasBackupHb = this.db.prepare("SELECT name FROM sqlite_master WHERE type='table' AND name = ?").get(backupHeartbeatsTable);
        if (hasBackupHb) {
          this.db.exec(`ALTER TABLE ${backupHeartbeatsTable} RENAME TO ${backupHeartbeatsTable}_migrated`);
          logger.info(`备份表 ${backupHeartbeatsTable} 已成功标记为已迁移`);
        }
      } catch (e) {}
      try {
        const backupIncidentsTable = backupMonitorsTable.replace('uptime_monitors_backup_', 'uptime_incidents_backup_');
        const hasBackupInc = this.db.prepare("SELECT name FROM sqlite_master WHERE type='table' AND name = ?").get(backupIncidentsTable);
        if (hasBackupInc) {
          this.db.exec(`ALTER TABLE ${backupIncidentsTable} RENAME TO ${backupIncidentsTable}_migrated`);
        }
      } catch (e) {}
    } catch (migrationErr) {
      logger.error('监控项数据迁移失败:', migrationErr.message);
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

          const hasThemeMode = settingsColumns.some(col => col.name === 'theme_mode');
          if (!hasThemeMode) {
            logger.info('正在为 user_settings 表添加 theme_mode 字段...');
            this.db.exec("ALTER TABLE user_settings ADD COLUMN theme_mode TEXT DEFAULT 'auto'");
            logger.success('user_settings.theme_mode 字段添加成功');
          } else {
            this.db
              .prepare("UPDATE user_settings SET theme_mode = 'auto' WHERE theme_mode IS NULL")
              .run();
          }

          const hasPageWidthMode = settingsColumns.some(col => col.name === 'page_width_mode');
          if (!hasPageWidthMode) {
            logger.info('正在为 user_settings 表添加 page_width_mode 字段...');
            this.db.exec(
              "ALTER TABLE user_settings ADD COLUMN page_width_mode TEXT DEFAULT 'standard'"
            );
            logger.success('user_settings.page_width_mode 字段添加成功');
          } else {
            this.db
              .prepare(
                "UPDATE user_settings SET page_width_mode = 'standard' WHERE page_width_mode IS NULL"
              )
              .run();
          }

          const hasSidebarCollapsed = settingsColumns.some(col => col.name === 'sidebar_collapsed');
          if (!hasSidebarCollapsed) {
            logger.info('正在为 user_settings 表添加 sidebar_collapsed 字段...');
            this.db.exec(
              'ALTER TABLE user_settings ADD COLUMN sidebar_collapsed INTEGER DEFAULT 0'
            );
            logger.success('user_settings.sidebar_collapsed 字段添加成功');
          } else {
            this.db
              .prepare('UPDATE user_settings SET sidebar_collapsed = 0 WHERE sidebar_collapsed IS NULL')
              .run();
          }
        }
      } catch (err) {
        logger.error('User Settings 额外字段迁移失败:', err.message);
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

          const hasFirstTokenTime = gcliLogColumns.some(col => col.name === 'first_token_time_ms');
          if (!hasFirstTokenTime) {
            logger.info('正在为 gemini_cli_logs 表添加 first_token_time_ms 字段...');
            this.db.exec('ALTER TABLE gemini_cli_logs ADD COLUMN first_token_time_ms INTEGER');
            logger.success('gemini_cli_logs.first_token_time_ms 字段添加成功');
          }

          const hasGcliTotalTokens = gcliLogColumns.some(col => col.name === 'total_tokens');
          if (!hasGcliTotalTokens) {
            logger.info('正在为 gemini_cli_logs 表添加 total_tokens 字段...');
            this.db.exec('ALTER TABLE gemini_cli_logs ADD COLUMN total_tokens INTEGER DEFAULT 0');
            logger.success('gemini_cli_logs.total_tokens 字段添加成功');
          }
        }
      } catch (err) {
        logger.error('Gemini CLI Logs 迁移失败:', err.message);
      }

      // DeepSeek Logs 迁移
      try {
        const dsLogColumns = this.db.pragma('table_info(ds_logs)');
        if (dsLogColumns.length > 0) {
          const hasTotalTokens = dsLogColumns.some(col => col.name === 'total_tokens');
          if (!hasTotalTokens) {
            logger.info('正在为 ds_logs 表添加 total_tokens 字段...');
            this.db.exec('ALTER TABLE ds_logs ADD COLUMN total_tokens INTEGER DEFAULT 0');
            logger.success('ds_logs.total_tokens 字段添加成功');
          }
        }
      } catch (err) {
        logger.error('DeepSeek Logs 迁移失败:', err.message);
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
            this.db.exec("ALTER TABLE server_accounts ADD COLUMN monitor_mode TEXT DEFAULT 'agent'");
            logger.success('server_accounts.monitor_mode 字段添加成功');
          }
          this.db.exec("UPDATE server_accounts SET monitor_mode = 'agent' WHERE monitor_mode IS NULL OR monitor_mode <> 'agent'");

          // 添加 country 字段
          const hasCountry = serverColumns.some(col => col.name === 'country');
          if (!hasCountry) {
            logger.info('正在为 server_accounts 表添加 country 字段...');
            this.db.exec('ALTER TABLE server_accounts ADD COLUMN country TEXT');
            logger.success('server_accounts.country 字段添加成功');
          }

          // 添加 resolved_country 字段
          const hasResolvedCountry = serverColumns.some(col => col.name === 'resolved_country');
          if (!hasResolvedCountry) {
            logger.info('正在为 server_accounts 表添加 resolved_country 字段...');
            this.db.exec('ALTER TABLE server_accounts ADD COLUMN resolved_country TEXT');
            logger.success('server_accounts.resolved_country 字段添加成功');
          }

          const hasExpiresAt = serverColumns.some(col => col.name === 'expires_at');
          if (!hasExpiresAt) {
            logger.info('正在为 server_accounts 表添加 expires_at 字段...');
            this.db.exec('ALTER TABLE server_accounts ADD COLUMN expires_at DATETIME');
            logger.success('server_accounts.expires_at 字段添加成功');
          }
        }
      } catch (err) {
        logger.error('Server Accounts 迁移失败:', err.message);
      }

      // Server Metrics History 迁移: 添加 platform 字段
      try {
        const metricsColumns = this.db.pragma('table_info(server_metrics_history)');
        if (metricsColumns.length > 0) {
          const hasCpuThreads = metricsColumns.some(col => col.name === 'cpu_threads');
          if (!hasCpuThreads) {
            logger.info('Adding server_metrics_history.cpu_threads column...');
            this.db.exec('ALTER TABLE server_metrics_history ADD COLUMN cpu_threads INTEGER DEFAULT 0');
            logger.success('server_metrics_history.cpu_threads column added');
          }

          const hasCpuTemp = metricsColumns.some(col => col.name === 'cpu_temp');
          if (!hasCpuTemp) {
            logger.info('Adding server_metrics_history.cpu_temp column...');
            this.db.exec('ALTER TABLE server_metrics_history ADD COLUMN cpu_temp REAL DEFAULT 0');
            logger.success('server_metrics_history.cpu_temp column added');
          }

          const hasCpuPower = metricsColumns.some(col => col.name === 'cpu_power');
          if (!hasCpuPower) {
            logger.info('Adding server_metrics_history.cpu_power column...');
            this.db.exec('ALTER TABLE server_metrics_history ADD COLUMN cpu_power REAL DEFAULT 0');
            logger.success('server_metrics_history.cpu_power column added');
          }

          const hasPlatform = metricsColumns.some(col => col.name === 'platform');
          if (!hasPlatform) {
            logger.info('正在为 server_metrics_history 表添加 platform 字段...');
            this.db.exec('ALTER TABLE server_metrics_history ADD COLUMN platform TEXT');
            logger.success('server_metrics_history.platform 字段添加成功');
          }

          const hasGpuTemp = metricsColumns.some(col => col.name === 'gpu_temp');
          if (!hasGpuTemp) {
            logger.info('Adding server_metrics_history.gpu_temp column...');
            this.db.exec('ALTER TABLE server_metrics_history ADD COLUMN gpu_temp REAL DEFAULT 0');
            logger.success('server_metrics_history.gpu_temp column added');
          }
        }
      } catch (err) {
        logger.error('Server Metrics History migration failed:', err.message);
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

      // TOTP secret 加密迁移
      try {
        const { isEncrypted, secureEncrypt } = require('../utils/secure-storage');
        const totpColumns = this.db.pragma('table_info(totp_accounts)');
        if (totpColumns.length > 0) {
          const hasSecretEncryptedAt = totpColumns.some(col => col.name === 'secret_encrypted_at');
          if (!hasSecretEncryptedAt) {
            logger.info('正在为 totp_accounts 表添加 secret_encrypted_at 字段...');
            this.db.exec('ALTER TABLE totp_accounts ADD COLUMN secret_encrypted_at DATETIME');
            logger.success('totp_accounts.secret_encrypted_at 字段添加成功');
          }

          const hasLastRevealedAt = totpColumns.some(col => col.name === 'last_revealed_at');
          if (!hasLastRevealedAt) {
            logger.info('正在为 totp_accounts 表添加 last_revealed_at 字段...');
            this.db.exec('ALTER TABLE totp_accounts ADD COLUMN last_revealed_at DATETIME');
            logger.success('totp_accounts.last_revealed_at 字段添加成功');
          }

          const accounts = this.db
            .prepare('SELECT id, secret FROM totp_accounts WHERE secret IS NOT NULL AND secret <> ?')
            .all('');
          const updateSecret = this.db.prepare(
            'UPDATE totp_accounts SET secret = ?, secret_encrypted_at = COALESCE(secret_encrypted_at, ?) WHERE id = ?'
          );
          const now = new Date().toISOString();
          const tx = this.db.transaction(() => {
            for (const account of accounts) {
              if (!isEncrypted(account.secret)) {
                updateSecret.run(secureEncrypt(account.secret), now, account.id);
              }
            }
          });
          tx();
        }
      } catch (err) {
        logger.error('TOTP secret 加密迁移失败:', err.message);
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

  getMigrationSelfCheck() {
    const db = this.getDatabase();
    const required = {
      user_settings: ['id', 'theme_mode', 'page_width_mode', 'sidebar_collapsed', 'module_visibility', 'module_order'],
      operation_logs: ['id', 'operation_type', 'table_name', 'trace_id'],
      totp_accounts: ['id', 'secret', 'secret_encrypted_at', 'last_revealed_at'],
      filebox_entries: ['code', 'type', 'expiry', 'downloads'],
      filebox_settings: ['id', 'max_file_size', 'allowed_mime_types', 'default_expiry_hours'],
      uptime_monitors: ['id', 'type', 'keyword', 'dns_resolve_type', 'config_json', 'push_token', 'push_grace_seconds'],
      uptime_monitor_states: ['monitor_id', 'state', 'fail_count', 'recover_count'],
      notification_channels: ['id', 'type', 'config'],
      settings_registry: ['domain', 'defaults_json', 'mask_fields_json'],
    };

    const result = {};
    for (const [table, columns] of Object.entries(required)) {
      const exists = db
        .prepare("SELECT name FROM sqlite_master WHERE type='table' AND name = ?")
        .get(table);
      if (!exists) {
        result[table] = { ok: false, missingColumns: columns, exists: false };
        continue;
      }
      const actualColumns = db.pragma(`table_info(${table})`).map(col => col.name);
      const missingColumns = columns.filter(column => !actualColumns.includes(column));
      result[table] = {
        ok: missingColumns.length === 0,
        exists: true,
        missingColumns,
      };
    }

    return {
      ok: Object.values(result).every(item => item.ok),
      checkedAt: new Date().toISOString(),
      tables: result,
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
