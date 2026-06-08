/**
 * Uptime 存储服务（SQLite 版）
 * 从 JSON 文件迁移到 SQLite，支持心跳持久化、Incident 事件、精确可用率计算
 */

const fs = require('fs');
const path = require('path');
const crypto = require('crypto');
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
            const addColumn = (table, column, definition) => {
                const info = db.prepare(`PRAGMA table_info(${table})`).all();
                if (!info.some(c => c.name === column)) {
                    logger.info(`正在为 ${table} 添加 ${column} 列...`);
                    db.prepare(`ALTER TABLE ${table} ADD COLUMN ${column} ${definition}`).run();
                }
            };

            addColumn('uptime_monitors', 'created_at', 'DATETIME DEFAULT CURRENT_TIMESTAMP');
            addColumn('uptime_monitors', 'updated_at', 'DATETIME DEFAULT CURRENT_TIMESTAMP');
            addColumn('uptime_monitors', 'keyword', 'TEXT');
            addColumn('uptime_monitors', 'dns_resolve_type', "TEXT DEFAULT 'A'");
            addColumn('uptime_monitors', 'dns_resolve_server', 'TEXT');
            addColumn('uptime_monitors', 'retry_interval', 'INTEGER DEFAULT 30');
            addColumn('uptime_monitors', 'resend_interval', 'INTEGER DEFAULT 0');
            addColumn('uptime_monitors', 'up_confirm_count', 'INTEGER');
            addColumn('uptime_monitors', 'down_confirm_count', 'INTEGER');
            addColumn('uptime_monitors', 'config_json', 'TEXT');
            addColumn('uptime_monitors', 'auth_json_encrypted', 'TEXT');
            addColumn('uptime_monitors', 'push_token', 'TEXT');
            addColumn('uptime_monitors', 'push_grace_seconds', 'INTEGER DEFAULT 120');
            addColumn('uptime_monitors', 'last_checked_at', 'DATETIME');
            addColumn('uptime_monitors', 'next_check_at', 'DATETIME');

            addColumn('uptime_heartbeats', 'state', 'TEXT');
            addColumn('uptime_heartbeats', 'duration_ms', 'INTEGER');
            addColumn('uptime_heartbeats', 'status_code', 'INTEGER');
            addColumn('uptime_heartbeats', 'error_code', 'TEXT');
            addColumn('uptime_heartbeats', 'details_json', 'TEXT');
            addColumn('uptime_heartbeats', 'maintenance', 'INTEGER DEFAULT 0');
            addColumn('uptime_heartbeats', 'probe_id', 'TEXT');

            addColumn('uptime_incidents', 'status', "TEXT DEFAULT 'open'");
            addColumn('uptime_incidents', 'severity', 'TEXT');
            addColumn('uptime_incidents', 'acknowledged_at', 'DATETIME');
            addColumn('uptime_incidents', 'acknowledged_by', 'TEXT');
            addColumn('uptime_incidents', 'maintenance_id', 'INTEGER');
            addColumn('uptime_incidents', 'resolved_reason', 'TEXT');

            db.exec(`
                CREATE TABLE IF NOT EXISTS uptime_monitor_states (
                    monitor_id INTEGER PRIMARY KEY,
                    state TEXT DEFAULT 'up',
                    fail_count INTEGER DEFAULT 0,
                    recover_count INTEGER DEFAULT 0,
                    active_incident_id INTEGER,
                    last_transition_at DATETIME,
                    last_error TEXT,
                    last_ping INTEGER DEFAULT 0,
                    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
                    FOREIGN KEY (monitor_id) REFERENCES uptime_monitors(id) ON DELETE CASCADE
                );
                CREATE TABLE IF NOT EXISTS uptime_groups (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    name TEXT NOT NULL,
                    order_index INTEGER DEFAULT 0,
                    color TEXT,
                    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
                );
                CREATE TABLE IF NOT EXISTS uptime_daily_stats (
                    monitor_id INTEGER NOT NULL,
                    date TEXT NOT NULL,
                    uptime REAL,
                    avg_latency REAL,
                    p95_latency REAL,
                    p99_latency REAL,
                    up_count INTEGER DEFAULT 0,
                    down_count INTEGER DEFAULT 0,
                    maintenance_count INTEGER DEFAULT 0,
                    incident_duration_ms INTEGER DEFAULT 0,
                    PRIMARY KEY (monitor_id, date)
                );
                CREATE TABLE IF NOT EXISTS uptime_status_pages (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    slug TEXT UNIQUE NOT NULL,
                    domain TEXT,
                    title TEXT NOT NULL,
                    description TEXT,
                    theme TEXT DEFAULT 'auto',
                    public INTEGER DEFAULT 1,
                    cache_seconds INTEGER DEFAULT 300,
                    config_json TEXT,
                    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
                    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
                );
                CREATE TABLE IF NOT EXISTS uptime_status_page_groups (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    status_page_id INTEGER NOT NULL,
                    name TEXT NOT NULL,
                    order_index INTEGER DEFAULT 0,
                    FOREIGN KEY (status_page_id) REFERENCES uptime_status_pages(id) ON DELETE CASCADE
                );
                CREATE TABLE IF NOT EXISTS uptime_status_page_monitors (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    status_page_id INTEGER NOT NULL,
                    group_id INTEGER,
                    monitor_id INTEGER NOT NULL,
                    order_index INTEGER DEFAULT 0,
                    display_name TEXT,
                    FOREIGN KEY (status_page_id) REFERENCES uptime_status_pages(id) ON DELETE CASCADE,
                    FOREIGN KEY (monitor_id) REFERENCES uptime_monitors(id) ON DELETE CASCADE
                );
                CREATE TABLE IF NOT EXISTS uptime_maintenance_windows (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    title TEXT NOT NULL,
                    description TEXT,
                    strategy TEXT DEFAULT 'manual',
                    timezone TEXT DEFAULT 'UTC',
                    start_at DATETIME,
                    end_at DATETIME,
                    cron TEXT,
                    recurrence_json TEXT,
                    active INTEGER DEFAULT 1,
                    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
                    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
                );
                CREATE TABLE IF NOT EXISTS uptime_maintenance_targets (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    maintenance_id INTEGER NOT NULL,
                    target_type TEXT NOT NULL,
                    target_id TEXT,
                    FOREIGN KEY (maintenance_id) REFERENCES uptime_maintenance_windows(id) ON DELETE CASCADE
                );
            `);
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
                // 利用 Set 内存去重旧配置插入
                const uniqueMonitors = [];
                const seenKeys = new Set();
                for (const m of oldMonitors) {
                    const key = `${m.name}|${m.type || 'http'}|${m.url || ''}|${m.hostname || ''}|${m.port || 0}`;
                    if (!seenKeys.has(key)) {
                        seenKeys.add(key);
                        uniqueMonitors.push(m);
                    }
                }

                logger.info(`正在迁移 ${uniqueMonitors.length} 个非重复监控项到 SQLite...`);
                const insert = db.prepare(`
                    INSERT INTO uptime_monitors (id, name, type, url, hostname, port, interval, timeout,
                        confirm_count, active, method, headers, body, ignore_tls, 
                        accepted_status_codes, keyword, dns_resolve_type, dns_resolve_server,
                        expiry_notification, notification_channels, tags, created_at)
                    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                `);

                const tx = db.transaction(() => {
                    for (const m of uniqueMonitors) {
                        insert.run(
                            m.id, m.name, m.type || 'http', m.url || null,
                            m.hostname || null, m.port || null, m.interval || 60,
                            m.timeout || 30, m.retries || 3, m.active ? 1 : 0,
                            m.method || 'GET', m.headers || null, m.body || null,
                            m.ignoreTls ? 1 : 0, m.accepted_status_codes || null,
                            m.keyword || null, m.dns_resolve_type || 'A', m.dns_resolve_server || null,
                            m.expiryNotification || 7,
                            JSON.stringify(m.notificationChannels || []),
                            JSON.stringify(m.tags || []),
                            m.createdAt || new Date().toISOString()
                        );
                    }
                });
                tx();
                logger.info(`✅ 迁移完成: ${uniqueMonitors.length} 个监控项`);

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

    getByPushToken(token) {
        this._checkColumns();
        this._ensureMigrated();
        if (!token) return null;
        const db = getDb();
        const m = db.prepare('SELECT * FROM uptime_monitors WHERE push_token = ? AND type = ?').get(token, 'push');
        return m ? this._parseMonitor(m) : null;
    }

    create(data) {
        this._checkColumns();
        this._ensureMigrated();
        const db = getDb();
        const config = this._normalizeMonitorConfig(data);
        const pushToken = data.type === 'push'
            ? (data.pushToken || data.push_token || this._generatePushToken())
            : (data.pushToken || data.push_token || null);

        // 强唯一去重防护：若相同监控特征的项已在 SQLite 中存在，直接返回已有监控项
        const existing = db.prepare(`
            SELECT id FROM uptime_monitors
            WHERE name = ? AND type = ? AND COALESCE(url, '') = ? AND COALESCE(hostname, '') = ? AND COALESCE(port, 0) = ?
        `).get(
            data.name,
            data.type || 'http',
            data.url || '',
            data.hostname || '',
            data.port || 0
        );

        if (existing) {
            logger.info(`检测到重复的监控项: [${data.name}], 返回已存在项 (ID: ${existing.id})`);
            return this.getById(existing.id);
        }

        const result = db.prepare(`
            INSERT INTO uptime_monitors (name, type, url, hostname, port, interval, timeout,
                confirm_count, active, method, headers, body, ignore_tls,
                accepted_status_codes, keyword, dns_resolve_type, dns_resolve_server,
                retry_interval, resend_interval, up_confirm_count, down_confirm_count,
                config_json, auth_json_encrypted, push_token, push_grace_seconds,
                expiry_notification, notification_channels, tags)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        `).run(
            data.name, data.type || 'http', data.url || null,
            data.hostname || null, data.port || null, data.interval || 60,
            data.timeout || 30, data.retries || data.confirmCount || 3,
            data.active !== undefined ? (data.active ? 1 : 0) : 1,
            data.method || 'GET', data.headers || null, data.body || null,
            data.ignoreTls ? 1 : 0, data.accepted_status_codes || null,
            data.keyword || null, data.dns_resolve_type || data.dnsResolveType || 'A',
            data.dns_resolve_server || data.dnsResolveServer || null,
            data.retryInterval || data.retry_interval || 30,
            data.resendInterval || data.resend_interval || 0,
            data.upConfirmCount || data.up_confirm_count || null,
            data.downConfirmCount || data.down_confirm_count || null,
            Object.keys(config).length > 0 ? JSON.stringify(config) : null,
            data.auth_json_encrypted || null,
            pushToken,
            data.pushGraceSeconds || data.push_grace_seconds || config.graceSeconds || 120,
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
            keyword: 'keyword',
            dns_resolve_type: 'dns_resolve_type',
            dnsResolveType: 'dns_resolve_type',
            dns_resolve_server: 'dns_resolve_server',
            dnsResolveServer: 'dns_resolve_server',
            retryInterval: 'retry_interval',
            retry_interval: 'retry_interval',
            resendInterval: 'resend_interval',
            resend_interval: 'resend_interval',
            upConfirmCount: 'up_confirm_count',
            up_confirm_count: 'up_confirm_count',
            downConfirmCount: 'down_confirm_count',
            down_confirm_count: 'down_confirm_count',
            expiryNotification: 'expiry_notification',
            pushToken: 'push_token',
            push_token: 'push_token',
            pushGraceSeconds: 'push_grace_seconds',
            push_grace_seconds: 'push_grace_seconds',
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
        if (data.config !== undefined || data.config_json !== undefined || data.jsonQueryPath !== undefined || data.jsonQueryOperator !== undefined || data.jsonExpectedValue !== undefined || data.pushGraceSeconds !== undefined) {
            fields.push('config_json = ?');
            values.push(JSON.stringify(this._normalizeMonitorConfig(data, id)));
        }
        if ((data.type === 'push' || this.getById(id)?.type === 'push') && !this.getById(id)?.pushToken && data.pushToken === undefined && data.push_token === undefined) {
            fields.push('push_token = ?');
            values.push(this._generatePushToken());
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
            keyword: row.keyword || '',
            dns_resolve_type: row.dns_resolve_type || 'A',
            dns_resolve_server: row.dns_resolve_server || '',
            retryInterval: row.retry_interval,
            resendInterval: row.resend_interval,
            upConfirmCount: row.up_confirm_count || row.confirm_count,
            downConfirmCount: row.down_confirm_count || row.confirm_count,
            config: this._parseJson(row.config_json, {}),
            pushToken: row.push_token || '',
            pushGraceSeconds: row.push_grace_seconds || 120,
            expiryNotification: row.expiry_notification,
            notificationChannels: this._parseJson(row.notification_channels, []),
            tags: this._parseJson(row.tags, []),
            jsonQueryPath: this._parseJson(row.config_json, {}).jsonQueryPath || '',
            jsonQueryOperator: this._parseJson(row.config_json, {}).jsonQueryOperator || 'equals',
            jsonExpectedValue: this._parseJson(row.config_json, {}).jsonExpectedValue || '',
        };
    }

    _parseJson(value, fallback) {
        if (!value) return fallback;
        try {
            return JSON.parse(value);
        } catch (e) {
            return fallback;
        }
    }

    _normalizeMonitorConfig(data = {}, existingId = null) {
        const existing = existingId ? this.getById(existingId) : null;
        const base = {
            ...(existing?.config || {}),
            ...(this._parseJson(data.config_json, {})),
            ...(data.config || {}),
        };

        if (data.jsonQueryPath !== undefined) base.jsonQueryPath = data.jsonQueryPath;
        if (data.jsonQueryOperator !== undefined) base.jsonQueryOperator = data.jsonQueryOperator;
        if (data.jsonExpectedValue !== undefined) base.jsonExpectedValue = data.jsonExpectedValue;
        if (data.expectedValue !== undefined) base.expectedValue = data.expectedValue;
        if (data.pushGraceSeconds !== undefined || data.push_grace_seconds !== undefined) {
            base.graceSeconds = data.pushGraceSeconds || data.push_grace_seconds;
        }

        return Object.fromEntries(
            Object.entries(base).filter(([, value]) => value !== undefined && value !== '')
        );
    }

    _generatePushToken() {
        return crypto.randomBytes(24).toString('base64url');
    }

    // ==================== 心跳记录 ====================

    saveHeartbeat(monitorId, beat) {
        try {
            const db = getDb();
            db.prepare(`
                INSERT INTO uptime_heartbeats (
                    monitor_id,
                    status,
                    state,
                    ping,
                    duration_ms,
                    status_code,
                    error_code,
                    details_json,
                    maintenance,
                    probe_id,
                    msg,
                    created_at
                )
                VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            `).run(
                monitorId,
                beat.status,
                beat.state || (beat.status === 1 ? 'up' : 'down'),
                beat.ping || 0,
                beat.durationMs || beat.ping || 0,
                beat.statusCode || null,
                beat.errorCode || null,
                beat.details ? JSON.stringify(beat.details) : null,
                beat.maintenance ? 1 : 0,
                beat.probeId || null,
                beat.msg || '',
                beat.time || new Date().toISOString()
            );
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

    getMonitorState(monitorId) {
        this._checkColumns();
        const db = getDb();
        return db.prepare(`
            SELECT
                monitor_id as monitorId,
                state,
                fail_count as failCount,
                recover_count as recoverCount,
                active_incident_id as activeIncidentId,
                last_transition_at as lastTransitionAt,
                last_error as lastError,
                last_ping as lastPing,
                updated_at as updatedAt
            FROM uptime_monitor_states
            WHERE monitor_id = ?
        `).get(monitorId) || null;
    }

    getAllStates() {
        this._checkColumns();
        const db = getDb();
        const rows = db.prepare(`
            SELECT
                monitor_id as monitorId,
                state,
                fail_count as failCount,
                recover_count as recoverCount,
                active_incident_id as activeIncidentId,
                last_transition_at as lastTransitionAt,
                last_error as lastError,
                last_ping as lastPing,
                updated_at as updatedAt
            FROM uptime_monitor_states
        `).all();
        return rows.reduce((acc, row) => {
            acc[row.monitorId] = row;
            return acc;
        }, {});
    }

    saveMonitorState(monitorId, state) {
        this._checkColumns();
        const db = getDb();
        db.prepare(`
            INSERT INTO uptime_monitor_states (
                monitor_id,
                state,
                fail_count,
                recover_count,
                active_incident_id,
                last_transition_at,
                last_error,
                last_ping,
                updated_at
            )
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
            ON CONFLICT(monitor_id) DO UPDATE SET
                state = excluded.state,
                fail_count = excluded.fail_count,
                recover_count = excluded.recover_count,
                active_incident_id = excluded.active_incident_id,
                last_transition_at = excluded.last_transition_at,
                last_error = excluded.last_error,
                last_ping = excluded.last_ping,
                updated_at = CURRENT_TIMESTAMP
        `).run(
            monitorId,
            state.state || state.status || 'up',
            state.failCount || 0,
            state.recoverCount || state.recoveryCount || 0,
            state.activeIncidentId || null,
            state.lastTransitionAt || new Date().toISOString(),
            state.lastError || null,
            state.lastPing || 0
        );
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

    listStatusPages() {
        this._checkColumns();
        return getDb()
            .prepare('SELECT * FROM uptime_status_pages ORDER BY created_at DESC')
            .all()
            .map(row => this._parseStatusPage(row));
    }

    getStatusPage(id) {
        this._checkColumns();
        const row = getDb().prepare('SELECT * FROM uptime_status_pages WHERE id = ?').get(id);
        return row ? this._parseStatusPage(row) : null;
    }

    getStatusPageBySlug(slug) {
        this._checkColumns();
        const row = getDb()
            .prepare('SELECT * FROM uptime_status_pages WHERE slug = ? AND public = 1')
            .get(this._normalizeSlug(slug));
        return row ? this._parseStatusPage(row) : null;
    }

    createStatusPage(data) {
        this._checkColumns();
        const db = getDb();
        const slug = this._normalizeSlug(data.slug || data.title);
        const tx = db.transaction(() => {
            const result = db.prepare(`
                INSERT INTO uptime_status_pages (
                    slug,
                    domain,
                    title,
                    description,
                    theme,
                    public,
                    cache_seconds,
                    config_json
                ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
            `).run(
                slug,
                data.domain || null,
                data.title || slug,
                data.description || '',
                data.theme || 'auto',
                data.public === false ? 0 : 1,
                data.cacheSeconds || data.cache_seconds || 300,
                data.config ? JSON.stringify(data.config) : null
            );
            this._replaceStatusPageMonitors(result.lastInsertRowid, data.monitorIds || data.monitor_ids || []);
            return result.lastInsertRowid;
        });
        return this.getStatusPage(tx());
    }

    updateStatusPage(id, data) {
        this._checkColumns();
        const db = getDb();
        const fields = [];
        const values = [];
        const map = {
            slug: 'slug',
            domain: 'domain',
            title: 'title',
            description: 'description',
            theme: 'theme',
            cacheSeconds: 'cache_seconds',
            cache_seconds: 'cache_seconds',
        };
        for (const [jsKey, dbKey] of Object.entries(map)) {
            if (data[jsKey] !== undefined) {
                fields.push(`${dbKey} = ?`);
                values.push(dbKey === 'slug' ? this._normalizeSlug(data[jsKey]) : data[jsKey]);
            }
        }
        if (data.public !== undefined) {
            fields.push('public = ?');
            values.push(data.public ? 1 : 0);
        }
        if (data.config !== undefined) {
            fields.push('config_json = ?');
            values.push(JSON.stringify(data.config));
        }
        if (fields.length > 0) {
            fields.push('updated_at = CURRENT_TIMESTAMP');
            values.push(id);
            db.prepare(`UPDATE uptime_status_pages SET ${fields.join(', ')} WHERE id = ?`).run(...values);
        }
        if (data.monitorIds || data.monitor_ids) {
            this._replaceStatusPageMonitors(id, data.monitorIds || data.monitor_ids || []);
        }
        return this.getStatusPage(id);
    }

    deleteStatusPage(id) {
        this._checkColumns();
        return getDb().prepare('DELETE FROM uptime_status_pages WHERE id = ?').run(id).changes > 0;
    }

    getPublicStatusPage(slug) {
        const page = this.getStatusPageBySlug(slug);
        if (!page) return null;
        const db = getDb();
        const monitors = db.prepare(`
            SELECT
                m.id,
                COALESCE(spm.display_name, m.name) as name,
                m.type,
                m.url,
                m.hostname,
                m.port,
                s.state,
                s.last_error,
                s.last_ping,
                s.updated_at
            FROM uptime_status_page_monitors spm
            JOIN uptime_monitors m ON m.id = spm.monitor_id
            LEFT JOIN uptime_monitor_states s ON s.monitor_id = m.id
            WHERE spm.status_page_id = ?
            ORDER BY spm.order_index ASC, m.name ASC
        `).all(page.id).map(monitor => ({
            id: monitor.id,
            name: monitor.name,
            type: monitor.type,
            target: monitor.url || [monitor.hostname, monitor.port].filter(Boolean).join(':'),
            state: monitor.state || 'unknown',
            lastError: monitor.last_error || null,
            lastPing: monitor.last_ping || 0,
            updatedAt: monitor.updated_at || null,
            uptime24h: this.calculateUptime(monitor.id, 1),
        }));

        return { ...page, monitors };
    }

    listMaintenanceWindows() {
        this._checkColumns();
        return getDb()
            .prepare('SELECT * FROM uptime_maintenance_windows ORDER BY created_at DESC')
            .all()
            .map(row => this._parseMaintenance(row));
    }

    createMaintenanceWindow(data) {
        this._checkColumns();
        const db = getDb();
        const tx = db.transaction(() => {
            const result = db.prepare(`
                INSERT INTO uptime_maintenance_windows (
                    title,
                    description,
                    strategy,
                    timezone,
                    start_at,
                    end_at,
                    cron,
                    recurrence_json,
                    active
                ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
            `).run(
                data.title,
                data.description || '',
                data.strategy || 'manual',
                data.timezone || 'UTC',
                data.startAt || data.start_at || null,
                data.endAt || data.end_at || null,
                data.cron || null,
                data.recurrence ? JSON.stringify(data.recurrence) : data.recurrence_json || null,
                data.active === false ? 0 : 1
            );
            this._replaceMaintenanceTargets(result.lastInsertRowid, data.targets || data.targetIds || []);
            return result.lastInsertRowid;
        });
        return this.getMaintenanceWindow(tx());
    }

    getMaintenanceWindow(id) {
        this._checkColumns();
        const row = getDb().prepare('SELECT * FROM uptime_maintenance_windows WHERE id = ?').get(id);
        return row ? this._parseMaintenance(row) : null;
    }

    updateMaintenanceWindow(id, data) {
        this._checkColumns();
        const fields = [];
        const values = [];
        const map = {
            title: 'title',
            description: 'description',
            strategy: 'strategy',
            timezone: 'timezone',
            startAt: 'start_at',
            start_at: 'start_at',
            endAt: 'end_at',
            end_at: 'end_at',
            cron: 'cron',
        };
        for (const [jsKey, dbKey] of Object.entries(map)) {
            if (data[jsKey] !== undefined) {
                fields.push(`${dbKey} = ?`);
                values.push(data[jsKey]);
            }
        }
        if (data.active !== undefined) {
            fields.push('active = ?');
            values.push(data.active ? 1 : 0);
        }
        if (data.recurrence !== undefined) {
            fields.push('recurrence_json = ?');
            values.push(JSON.stringify(data.recurrence));
        }
        if (fields.length > 0) {
            fields.push('updated_at = CURRENT_TIMESTAMP');
            values.push(id);
            getDb().prepare(`UPDATE uptime_maintenance_windows SET ${fields.join(', ')} WHERE id = ?`).run(...values);
        }
        if (data.targets || data.targetIds) {
            this._replaceMaintenanceTargets(id, data.targets || data.targetIds || []);
        }
        return this.getMaintenanceWindow(id);
    }

    deleteMaintenanceWindow(id) {
        this._checkColumns();
        return getDb().prepare('DELETE FROM uptime_maintenance_windows WHERE id = ?').run(id).changes > 0;
    }

    getActiveMaintenanceForMonitor(monitorId) {
        this._checkColumns();
        const now = new Date().toISOString();
        return getDb().prepare(`
            SELECT mw.*
            FROM uptime_maintenance_windows mw
            LEFT JOIN uptime_maintenance_targets mt ON mt.maintenance_id = mw.id
            WHERE mw.active = 1
              AND (mw.start_at IS NULL OR mw.start_at <= ?)
              AND (mw.end_at IS NULL OR mw.end_at >= ?)
              AND (
                mt.id IS NULL
                OR mt.target_type = 'global'
                OR (mt.target_type = 'monitor' AND mt.target_id = ?)
              )
            ORDER BY mw.created_at DESC
            LIMIT 1
        `).get(now, now, String(monitorId)) || null;
    }

    _replaceStatusPageMonitors(statusPageId, monitorIds) {
        const db = getDb();
        db.prepare('DELETE FROM uptime_status_page_monitors WHERE status_page_id = ?').run(statusPageId);
        const insert = db.prepare(`
            INSERT INTO uptime_status_page_monitors (status_page_id, monitor_id, order_index)
            VALUES (?, ?, ?)
        `);
        monitorIds.map(Number).filter(Boolean).forEach((monitorId, index) => {
            insert.run(statusPageId, monitorId, index);
        });
    }

    _replaceMaintenanceTargets(maintenanceId, targets) {
        const db = getDb();
        db.prepare('DELETE FROM uptime_maintenance_targets WHERE maintenance_id = ?').run(maintenanceId);
        const normalized = Array.isArray(targets) ? targets : [];
        if (normalized.length === 0) {
            db.prepare(`
                INSERT INTO uptime_maintenance_targets (maintenance_id, target_type, target_id)
                VALUES (?, 'global', NULL)
            `).run(maintenanceId);
            return;
        }
        const insert = db.prepare(`
            INSERT INTO uptime_maintenance_targets (maintenance_id, target_type, target_id)
            VALUES (?, ?, ?)
        `);
        normalized.forEach(target => {
            if (typeof target === 'object') {
                insert.run(maintenanceId, target.type || target.targetType || 'monitor', target.id || target.targetId || null);
            } else {
                insert.run(maintenanceId, 'monitor', String(target));
            }
        });
    }

    _getStatusPageMonitorIds(statusPageId) {
        return getDb()
            .prepare('SELECT monitor_id FROM uptime_status_page_monitors WHERE status_page_id = ? ORDER BY order_index ASC')
            .all(statusPageId)
            .map(row => row.monitor_id);
    }

    _getMaintenanceTargets(maintenanceId) {
        return getDb()
            .prepare('SELECT target_type as type, target_id as id FROM uptime_maintenance_targets WHERE maintenance_id = ? ORDER BY id ASC')
            .all(maintenanceId)
            .map(row => ({ type: row.type, id: row.id }));
    }

    _parseStatusPage(row) {
        return {
            id: row.id,
            slug: row.slug,
            domain: row.domain,
            title: row.title,
            description: row.description || '',
            theme: row.theme || 'auto',
            public: row.public === 1,
            cacheSeconds: row.cache_seconds || 300,
            config: this._parseJson(row.config_json, {}),
            monitorIds: this._getStatusPageMonitorIds(row.id),
            createdAt: row.created_at,
            updatedAt: row.updated_at,
        };
    }

    _parseMaintenance(row) {
        return {
            id: row.id,
            title: row.title,
            description: row.description || '',
            strategy: row.strategy || 'manual',
            timezone: row.timezone || 'UTC',
            startAt: row.start_at,
            endAt: row.end_at,
            cron: row.cron,
            recurrence: this._parseJson(row.recurrence_json, null),
            targets: this._getMaintenanceTargets(row.id),
            active: row.active === 1,
            createdAt: row.created_at,
            updatedAt: row.updated_at,
        };
    }

    _normalizeSlug(value) {
        return String(value || 'status')
            .trim()
            .toLowerCase()
            .replace(/[^a-z0-9-]+/g, '-')
            .replace(/^-+|-+$/g, '')
            || 'status';
    }
}

module.exports = new UptimeStorage();
