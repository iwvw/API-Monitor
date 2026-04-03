/**
 * Uptime 存储服务（SQLite 版）
 * 从 JSON 文件迁移到 SQLite，支持心跳持久化、Incident 事件、精确可用率计算
 */

const fs = require('fs');
const path = require('path');
const { createLogger } = require('../../src/utils/logger');

const logger = createLogger('UptimeStorage');

function getDb() {
    return require('../../src/db/database').getDatabase();
}

class UptimeStorage {
    constructor() {
        // 延迟初始化，在首次调用时检查迁移
        this._migrated = false;
        this._columnsChecked = false;
    }

    _checkColumns() {
        if (this._columnsChecked) return;
        this._columnsChecked = true;
        const db = getDb();
        try {
            // 检查 uptime_monitors 是否有 created_at 列
            const info = db.prepare('PRAGMA table_info(uptime_monitors)').all();
            const hasCreatedAt = info.some(c => c.name === 'created_at');
            if (!hasCreatedAt) {
                logger.info('正在为 uptime_monitors 添加 created_at 和 updated_at 列...');
                db.prepare('ALTER TABLE uptime_monitors ADD COLUMN created_at DATETIME DEFAULT CURRENT_TIMESTAMP').run();
                db.prepare('ALTER TABLE uptime_monitors ADD COLUMN updated_at DATETIME DEFAULT CURRENT_TIMESTAMP').run();
            }
        } catch (e) {
            logger.warn(`检查列定义失败: ${e.message}`);
        }
    }

    /**
     * 确保旧数据已迁移
     */
    _ensureMigrated() {
        if (this._migrated) return;
        this._migrated = true;

        try {
            const db = getDb();
            const count = db.prepare('SELECT COUNT(*) as c FROM uptime_monitors').get();
            if (count.c > 0) return; // 已有数据，无需迁移

            // 尝试从旧的 SystemConfig 或 JSON 文件迁移
            const { SystemConfig } = require('../../src/db/models');
            const data = SystemConfig.getConfigValue('uptime_monitors_json');
            let oldMonitors = [];

            if (data) {
                oldMonitors = JSON.parse(data);
            } else {
                const oldFile = path.join(__dirname, '../../data/uptime-monitors.json');
                if (fs.existsSync(oldFile)) {
                    oldMonitors = JSON.parse(fs.readFileSync(oldFile, 'utf8'));
                }
            }

            if (oldMonitors.length > 0) {
                logger.info(`正在迁移 ${oldMonitors.length} 个监控项到 SQLite...`);
                const insert = db.prepare(`
                    INSERT INTO uptime_monitors (id, name, type, url, hostname, port, interval, timeout,
                        confirm_count, active, method, headers, body, ignore_tls, 
                        accepted_status_codes, expiry_notification, notification_channels, tags, created_at)
                    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                `);

                const tx = db.transaction(() => {
                    for (const m of oldMonitors) {
                        insert.run(
                            m.id, m.name, m.type || 'http', m.url || null,
                            m.hostname || null, m.port || null, m.interval || 60,
                            m.timeout || 30, m.retries || 3, m.active ? 1 : 0,
                            m.method || 'GET', m.headers || null, m.body || null,
                            m.ignoreTls ? 1 : 0, m.accepted_status_codes || null,
                            m.expiryNotification || 7,
                            JSON.stringify(m.notificationChannels || []),
                            JSON.stringify(m.tags || []),
                            m.createdAt || new Date().toISOString()
                        );
                    }
                });
                tx();
                logger.info(`✅ 迁移完成: ${oldMonitors.length} 个监控项`);

                // 迁移心跳历史
                this._migrateHeartbeats(oldMonitors);
            }
        } catch (e) {
            logger.warn(`数据迁移检查: ${e.message}`);
        }
    }

    /**
     * 迁移旧版心跳 JSON 文件到 SQLite
     */
    _migrateHeartbeats(monitors) {
        try {
            const db = getDb();
            const historyDir = path.join(__dirname, '../../data/uptime-history');
            if (!fs.existsSync(historyDir)) return;

            const insert = db.prepare(`
                INSERT INTO uptime_heartbeats (monitor_id, status, ping, msg, created_at)
                VALUES (?, ?, ?, ?, ?)
            `);

            let total = 0;
            const tx = db.transaction(() => {
                for (const m of monitors) {
                    const file = path.join(historyDir, `${m.id}.json`);
                    if (!fs.existsSync(file)) continue;

                    try {
                        const beats = JSON.parse(fs.readFileSync(file, 'utf8'));
                        for (const b of beats) {
                            insert.run(m.id, b.status, b.ping || 0, b.msg || '', b.time || new Date().toISOString());
                            total++;
                        }
                    } catch (e) { /* 忽略单个文件错误 */ }
                }
            });
            tx();

            if (total > 0) {
                logger.info(`✅ 迁移心跳记录: ${total} 条`);
            }
        } catch (e) {
            logger.warn(`心跳迁移: ${e.message}`);
        }
    }

    // ==================== 监控项 CRUD ====================

    getAll() {
        this._checkColumns();
        this._ensureMigrated();
        const db = getDb();
        const monitors = db.prepare('SELECT * FROM uptime_monitors ORDER BY created_at DESC').all();
        return monitors.map(m => this._parseMonitor(m));
    }

    getActive() {
        this._checkColumns();
        this._ensureMigrated();
        const db = getDb();
        const monitors = db.prepare('SELECT * FROM uptime_monitors WHERE active = 1').all();
        return monitors.map(m => this._parseMonitor(m));
    }

    getById(id) {
        this._checkColumns();
        this._ensureMigrated();
        const db = getDb();
        const m = db.prepare('SELECT * FROM uptime_monitors WHERE id = ?').get(id);
        return m ? this._parseMonitor(m) : null;
    }

    create(data) {
        this._checkColumns();
        this._ensureMigrated();
        const db = getDb();
        const result = db.prepare(`
            INSERT INTO uptime_monitors (name, type, url, hostname, port, interval, timeout,
                confirm_count, active, method, headers, body, ignore_tls,
                accepted_status_codes, expiry_notification, notification_channels, tags)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        `).run(
            data.name, data.type || 'http', data.url || null,
            data.hostname || null, data.port || null, data.interval || 60,
            data.timeout || 30, data.retries || data.confirmCount || 3,
            data.active !== undefined ? (data.active ? 1 : 0) : 1,
            data.method || 'GET', data.headers || null, data.body || null,
            data.ignoreTls ? 1 : 0, data.accepted_status_codes || null,
            data.expiryNotification || 7,
            JSON.stringify(data.notificationChannels || []),
            JSON.stringify(data.tags || [])
        );

        return this.getById(result.lastInsertRowid);
    }

    update(id, data) {
        this._checkColumns();
        this._ensureMigrated();
        const db = getDb();
        const fields = [];
        const values = [];

        const fieldMap = {
            name: 'name', type: 'type', url: 'url', hostname: 'hostname',
            port: 'port', interval: 'interval', timeout: 'timeout',
            method: 'method', headers: 'headers', body: 'body',
            accepted_status_codes: 'accepted_status_codes',
            expiryNotification: 'expiry_notification',
        };

        for (const [jsKey, dbKey] of Object.entries(fieldMap)) {
            if (data[jsKey] !== undefined) {
                fields.push(`${dbKey} = ?`);
                values.push(data[jsKey]);
            }
        }

        // 布尔/特殊字段
        if (data.active !== undefined) { fields.push('active = ?'); values.push(data.active ? 1 : 0); }
        if (data.ignoreTls !== undefined) { fields.push('ignore_tls = ?'); values.push(data.ignoreTls ? 1 : 0); }
        if (data.retries !== undefined || data.confirmCount !== undefined) {
            fields.push('confirm_count = ?');
            values.push(data.retries || data.confirmCount || 3);
        }
        if (data.notificationChannels !== undefined) {
            fields.push('notification_channels = ?');
            values.push(JSON.stringify(data.notificationChannels));
        }
        if (data.tags !== undefined) {
            fields.push('tags = ?');
            values.push(JSON.stringify(data.tags));
        }

        if (fields.length === 0) return this.getById(id);

        fields.push('updated_at = CURRENT_TIMESTAMP');
        values.push(id);

        db.prepare(`UPDATE uptime_monitors SET ${fields.join(', ')} WHERE id = ?`).run(...values);
        return this.getById(id);
    }

    delete(id) {
        this._ensureMigrated();
        const db = getDb();
        const result = db.prepare('DELETE FROM uptime_monitors WHERE id = ?').run(id);
        return result.changes > 0;
    }

    /**
     * 解析数据库行到前端友好的格式
     */
    _parseMonitor(row) {
        return {
            ...row,
            active: row.active === 1,
            ignoreTls: row.ignore_tls === 1,
            confirmCount: row.confirm_count,
            retries: row.confirm_count, // 前端兼容
            expiryNotification: row.expiry_notification,
            notificationChannels: JSON.parse(row.notification_channels || '[]'),
            tags: JSON.parse(row.tags || '[]'),
        };
    }

    // ==================== 心跳记录 ====================

    saveHeartbeat(monitorId, beat) {
        try {
            const db = getDb();
            db.prepare(`
                INSERT INTO uptime_heartbeats (monitor_id, status, ping, msg, created_at)
                VALUES (?, ?, ?, ?, ?)
            `).run(monitorId, beat.status, beat.ping || 0, beat.msg || '', beat.time || new Date().toISOString());
        } catch (e) {
            // 表可能尚未创建
        }
    }

    getLastHeartbeat(monitorId) {
        try {
            const db = getDb();
            return db.prepare(`
                SELECT status, ping, msg, created_at as time
                FROM uptime_heartbeats
                WHERE monitor_id = ?
                ORDER BY created_at DESC LIMIT 1
            `).get(monitorId) || null;
        } catch (e) {
            return null;
        }
    }

    getHistory(monitorId, limit = 60) {
        try {
            const db = getDb();
            return db.prepare(`
                SELECT status, ping, msg, created_at as time
                FROM uptime_heartbeats
                WHERE monitor_id = ?
                ORDER BY created_at DESC LIMIT ?
            `).all(monitorId, limit);
        } catch (e) {
            return [];
        }
    }

    /**
     * 清理旧心跳（保留最近 N 天）
     */
    cleanOldHeartbeats(retentionDays = 7) {
        const db = getDb();
        const result = db.prepare(`
            DELETE FROM uptime_heartbeats
            WHERE created_at < datetime('now', '-' || ? || ' days')
        `).run(retentionDays);
        return result.changes;
    }

    // ==================== Incident 事件 ====================

    createIncident(monitorId, cause) {
        const db = getDb();
        const result = db.prepare(`
            INSERT INTO uptime_incidents (monitor_id, started_at, cause)
            VALUES (?, datetime('now'), ?)
        `).run(monitorId, cause || 'Unknown');
        return result.lastInsertRowid;
    }

    resolveIncident(monitorId, durationMs) {
        const db = getDb();
        // 找到该 monitor 最新的未关闭 incident
        const incident = db.prepare(`
            SELECT id FROM uptime_incidents
            WHERE monitor_id = ? AND resolved_at IS NULL
            ORDER BY started_at DESC LIMIT 1
        `).get(monitorId);

        if (incident) {
            db.prepare(`
                UPDATE uptime_incidents
                SET resolved_at = datetime('now'), duration_ms = ?
                WHERE id = ?
            `).run(durationMs, incident.id);
        }
        return incident;
    }

    getOpenIncident(monitorId) {
        const db = getDb();
        return db.prepare(`
            SELECT * FROM uptime_incidents
            WHERE monitor_id = ? AND resolved_at IS NULL
            ORDER BY started_at DESC LIMIT 1
        `).get(monitorId) || null;
    }

    getIncidents(monitorId, limit = 20) {
        const db = getDb();
        return db.prepare(`
            SELECT * FROM uptime_incidents
            WHERE monitor_id = ?
            ORDER BY started_at DESC LIMIT ?
        `).all(monitorId, limit);
    }

    /**
     * 精确计算可用率 = (总时间 - 总宕机时间) / 总时间 × 100
     */
    calculateUptime(monitorId, days = 1) {
        const db = getDb();
        const totalMs = days * 24 * 60 * 60 * 1000;
        const since = new Date(Date.now() - totalMs).toISOString();

        // 获取该时间段内的所有 incident
        const incidents = db.prepare(`
            SELECT started_at, COALESCE(resolved_at, datetime('now')) as resolved_at
            FROM uptime_incidents
            WHERE monitor_id = ? AND (resolved_at > ? OR resolved_at IS NULL)
        `).all(monitorId, since);

        let downMs = 0;
        const rangeStart = Date.now() - totalMs;

        for (const inc of incidents) {
            const start = Math.max(new Date(inc.started_at).getTime(), rangeStart);
            const end = Math.min(new Date(inc.resolved_at).getTime(), Date.now());
            if (end > start) {
                downMs += end - start;
            }
        }

        return ((1 - downMs / totalMs) * 100).toFixed(3);
    }
}

module.exports = new UptimeStorage();
