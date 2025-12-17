const Database = require('better-sqlite3');
const fs = require('fs');
const path = require('path');
const { createLogger } = require('../utils/logger');

const logger = createLogger('Database');

class DatabaseService {
    constructor() {
        this.db = null;
        this.initialized = false;
        this.dbPath = path.join(__dirname, '../../data/api-monitor.db');
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
}

// 导出单例实例
const dbService = new DatabaseService();
module.exports = dbService;
