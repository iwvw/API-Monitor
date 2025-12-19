const Database = require('better-sqlite3');
const fs = require('fs');
const path = require('path');
const { createLogger } = require('../utils/logger');

const logger = createLogger('Database');

class DatabaseService {
    constructor() {
        this.db = null;
        this.initialized = false;
        this.dbPath = path.join(__dirname, '../../data/data.db');
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
                verbose: process.env.NODE_ENV === 'development' ? null : null
            });

            // 启用外键约束
            this.db.pragma('foreign_keys = ON');

            // 启用 WAL 模式 (提升并发性能)
            this.db.pragma('journal_mode = WAL');
            this.db.pragma('synchronous = NORMAL');

            // 执行数据库初始化脚本
            this.initializeSchema();

            this.initialized = true;
            logger.success('数据库连接就绪: ' + path.basename(this.dbPath));

            return this.db;
        } catch (error) {
            logger.error('数据库初始化失败', error.message);
            throw error;
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
                const modules = fs.readdirSync(modulesDir, { withFileTypes: true })
                    .filter(dirent => dirent.isDirectory())
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
                    const hasCloudaicompanion = gcliColumns.some(col => col.name === 'cloudaicompanion_project_id');
                    if (!hasCloudaicompanion) {
                        logger.info('正在为 gemini_cli_accounts 表添加 cloudaicompanion_project_id 字段...');
                        this.db.exec('ALTER TABLE gemini_cli_accounts ADD COLUMN cloudaicompanion_project_id TEXT');
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

                    const hasLoadStrategy = settingsColumns.some(col => col.name === 'load_balancing_strategy');
                    if (!hasLoadStrategy) {
                        logger.info('正在为 user_settings 表添加 load_balancing_strategy 字段...');
                        this.db.exec("ALTER TABLE user_settings ADD COLUMN load_balancing_strategy TEXT DEFAULT 'random'");
                        logger.success('user_settings.load_balancing_strategy 字段添加成功');
                    }

                    const hasIpDisplayMode = settingsColumns.some(col => col.name === 'server_ip_display_mode');
                    if (!hasIpDisplayMode) {
                        logger.info('正在为 user_settings 表添加 server_ip_display_mode 字段...');
                        this.db.exec("ALTER TABLE user_settings ADD COLUMN server_ip_display_mode TEXT DEFAULT 'normal'");
                        logger.success('user_settings.server_ip_display_mode 字段添加成功');
                    }
                }
            } catch (err) {
                logger.error('User Settings 额外字段迁移失败:', err.message);
            }

            // Operation Logs 迁移: 检查 operation_logs 表是否有 trace_id 字段
            try {
                const logColumns = this.db.pragma('table_info(operation_logs)');
                if (logColumns.length > 0) {
                    const hasTraceId = logColumns.some(col => col.name === 'trace_id');
                    if (!hasTraceId) {
                        logger.info('正在为 operation_logs 表添加 trace_id 字段...');
                        this.db.exec("ALTER TABLE operation_logs ADD COLUMN trace_id TEXT");
                        logger.success('operation_logs.trace_id 字段添加成功');
                    }
                }
            } catch (err) {
                logger.error('Operation Logs 额外字段迁移失败:', err.message);
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
     * 关闭数据库连接
     */
    close() {
        if (this.db) {
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
            // 使用文件复制方式备份，更可靠
            // 先确保数据库已同步到磁盘
            const db = this.getDatabase();
            db.pragma('wal_checkpoint(TRUNCATE)');

            // 复制数据库文件
            await fs.promises.copyFile(this.dbPath, backupPath);

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

        const tables = db.prepare(`
            SELECT name FROM sqlite_master
            WHERE type='table' AND name NOT LIKE 'sqlite_%'
        `).all();

        const stats = {};
        tables.forEach(({ name }) => {
            const count = db.prepare(`SELECT COUNT(*) as count FROM ${name}`).get();
            stats[name] = count.count;
        });

        return {
            dbPath: this.dbPath,
            dbSize: fs.statSync(this.dbPath).size,
            tables: stats
        };
    }

    /**
     * 清空所有表数据（保留表结构）
     */
    clearAllData() {
        const db = this.getDatabase();

        const tables = db.prepare(`
            SELECT name FROM sqlite_master
            WHERE type='table' AND name NOT LIKE 'sqlite_%'
        `).all();

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
     */
    vacuum() {
        try {
            const db = this.getDatabase();
            logger.info('开始执行数据库 VACUUM...');
            db.exec('VACUUM');
            logger.success('数据库 VACUUM 完成');
            return true;
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
            const tables = db.prepare(`
                SELECT name FROM sqlite_master 
                WHERE type='table' AND (name LIKE '%_logs' OR name LIKE '%_history')
            `).all();

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
            const tables = db.prepare(`
                SELECT name FROM sqlite_master 
                WHERE type='table' AND (name LIKE '%_logs' OR name LIKE '%_history')
            `).all();

            if (tables.length === 0) return { deleted: 0 };

            const transaction = db.transaction(() => {
                tables.forEach(({ name }) => {
                    // Determine timestamp column name
                    const columns = db.pragma(`table_info(${name})`);
                    const timeCol = columns.find(c => ['created_at', 'checked_at', 'timestamp'].includes(c.name))?.name;

                    if (!timeCol) {
                        logger.warn(`跳过表 ${name}: 未找到时间戳字段`);
                        return;
                    }

                    // 1. 按天数清理
                    if (days > 0) {
                        const result = db.prepare(`
                            DELETE FROM ${name} 
                            WHERE ${timeCol} < datetime('now', '-${days} days')
                        `).run();
                        if (result.changes > 0) {
                            logger.debug(`[${name}] 清理过期日志(${days}天): ${result.changes}条`);
                            totalDeleted += result.changes;
                        }
                    }

                    // 2. 按数量清理 (保留最新的 N 条)
                    if (count > 0) {
                        // SQLite DELETE limit 语法比较特殊，通常用 subquery
                        const result = db.prepare(`
                            DELETE FROM ${name} 
                            WHERE rowid NOT IN (
                                SELECT rowid FROM ${name} 
                                ORDER BY ${timeCol} DESC 
                                LIMIT ?
                            )
                        `).run(count);

                        if (result.changes > 0) {
                            logger.debug(`[${name}] 清理超量日志(保留${count}条): ${result.changes}条`);
                            totalDeleted += result.changes;
                        }
                    }
                });
            });

            transaction();

            // 3. 按数据库大小清理 (如果超出限制，触发 VACUUM 并再次检查? 或者简单地警告?)
            // 实现策略：如果文件大小 > 限制，尝试 VACUUM。如果还大，只能删数据(暂不实现自动删数据以防误删，只做 VACUUM)
            if (dbSizeMB > 0) {
                const stats = fs.statSync(this.dbPath);
                const sizeMB = stats.size / (1024 * 1024);

                if (sizeMB > dbSizeMB) {
                    logger.warn(`数据库大小 (${sizeMB.toFixed(2)}MB) 超过限制 (${dbSizeMB}MB)，尝试执行 VACUUM...`);
                    db.exec('VACUUM');

                    // 再次检查
                    const newStats = fs.statSync(this.dbPath);
                    const newSizeMB = newStats.size / (1024 * 1024);
                    logger.info(`VACUUM 完成。当前大小: ${newSizeMB.toFixed(2)}MB`);
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
