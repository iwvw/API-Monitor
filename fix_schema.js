const dbService = require('./src/db/database');
const { createLogger } = require('./src/utils/logger');

const logger = createLogger('SchemaMigration');

async function migrate() {
    try {
        logger.info('开始检查数据库架构...');

        // 初始化数据库连接
        dbService.initialize();
        const db = dbService.getDb();

        // 检查 user_settings 表信息
        const tableInfo = db.pragma('table_info(user_settings)');
        const hasColumn = tableInfo.some(col => col.name === 'zeabur_refresh_interval');

        if (!hasColumn) {
            logger.info('检测到缺失列: zeabur_refresh_interval，正在添加...');

            // 添加列
            db.prepare('ALTER TABLE user_settings ADD COLUMN zeabur_refresh_interval INTEGER DEFAULT 30000').run();

            logger.success('成功添加列: zeabur_refresh_interval');
        } else {
            logger.info('列 zeabur_refresh_interval 已存在，无需操作');
        }

    } catch (error) {
        logger.error('迁移失败', error.message);
    } finally {
        // 不需要显式关闭 better-sqlite3
        process.exit(0);
    }
}

migrate();
