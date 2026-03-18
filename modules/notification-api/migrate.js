/**
 * 通知系统数据库迁移脚本
 * 为已存在的表添加缺失的列
 */

const { createLogger } = require('../../src/utils/logger');
const logger = createLogger('NotifMigrate');

function migrate(db) {
    if (!db) return;

    const migrations = [
        // alert_rules 缺失列
        { table: 'alert_rules', column: 'title_template', sql: "ALTER TABLE alert_rules ADD COLUMN title_template TEXT DEFAULT ''" },
        { table: 'alert_rules', column: 'message_template', sql: "ALTER TABLE alert_rules ADD COLUMN message_template TEXT DEFAULT ''" },
        { table: 'alert_rules', column: 'backup_channels', sql: "ALTER TABLE alert_rules ADD COLUMN backup_channels TEXT DEFAULT '[]'" },
        // alert_state_tracking 缺失列
        { table: 'alert_state_tracking', column: 'state_history', sql: "ALTER TABLE alert_state_tracking ADD COLUMN state_history TEXT DEFAULT '[]'" },
        { table: 'alert_state_tracking', column: 'is_flapping', sql: "ALTER TABLE alert_state_tracking ADD COLUMN is_flapping INTEGER DEFAULT 0" },
    ];

    for (const m of migrations) {
        try {
            // 检查列是否已存在
            const columns = db.pragma(`table_info(${m.table})`);
            const exists = columns.some(c => c.name === m.column);
            if (!exists) {
                db.exec(m.sql);
                logger.info(`迁移: ${m.table} 添加列 ${m.column}`);
            }
        } catch (e) {
            // 列已存在或其他错误，忽略
            if (!e.message.includes('duplicate column')) {
                logger.warn(`迁移警告 (${m.table}.${m.column}): ${e.message}`);
            }
        }
    }
}

module.exports = { migrate };
