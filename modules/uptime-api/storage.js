/**
 * Uptime 存储服务
 * 处理监控项和心跳历史数据的 JSON 文件持久化
 */

const fs = require('fs');
const path = require('path');
const { createLogger } = require('../../src/utils/logger');

const logger = createLogger('UptimeStorage');
const DATA_DIR = path.join(__dirname, '../../data');
const HISTORY_DIR = path.join(DATA_DIR, 'uptime-history');

// 确保数据目录存在
if (!fs.existsSync(DATA_DIR)) fs.mkdirSync(DATA_DIR, { recursive: true });
if (!fs.existsSync(HISTORY_DIR)) fs.mkdirSync(HISTORY_DIR, { recursive: true });

class UptimeStorage {
    constructor() {
        this.monitors = [];
        this.loadMonitors();
    }

    loadMonitors() {
        try {
            const { SystemConfig } = require('../../src/db/models');
            const data = SystemConfig.getConfigValue('uptime_monitors_json');
            if (data) {
                this.monitors = JSON.parse(data);
            } else {
                // 尝试从旧文件迁移
                const oldFile = path.join(__dirname, '../../data/uptime-monitors.json');
                if (fs.existsSync(oldFile)) {
                    const fileContent = fs.readFileSync(oldFile, 'utf8');
                    this.monitors = JSON.parse(fileContent);
                    SystemConfig.setConfig('uptime_monitors_json', fileContent);
                    logger.info('Migrated uptime monitors from JSON file to database');
                    try { fs.renameSync(oldFile, oldFile + '.bak'); } catch (e) { }
                } else {
                    this.monitors = [];
                    this.saveMonitors();
                }
            }
        } catch (error) {
            logger.error('Failed to load monitors:', error);
            this.monitors = [];
        }
    }

    saveMonitors() {
        try {
            const { SystemConfig } = require('../../src/db/models');
            SystemConfig.setConfig('uptime_monitors_json', JSON.stringify(this.monitors));
        } catch (error) {
            logger.error('Failed to save monitors:', error);
        }
    }

    /**
     * 获取所有监控项
     */
    getAll() {
        return this.monitors;
    }

    /**
     * 获取活跃的监控项
     */
    getActive() {
        return this.monitors.filter(m => m.active);
    }

    /**
     * 获取单个监控项
     */
    getById(id) {
        return this.monitors.find(m => m.id == id);
    }

    /**
     * 创建监控项
     */
    create(data) {
        const newMonitor = {
            ...data,
            id: Date.now(), // 简单 ID 生成
            createdAt: new Date().toISOString()
        };
        this.monitors.push(newMonitor);
        this.saveMonitors();
        return newMonitor;
    }

    /**
     * 更新监控项
     */
    update(id, data) {
        const index = this.monitors.findIndex(m => m.id == id);
        if (index !== -1) {
            this.monitors[index] = { ...this.monitors[index], ...data };
            this.saveMonitors();
            return this.monitors[index];
        }
        return null;
    }

    /**
     * 删除监控项
     */
    delete(id) {
        const index = this.monitors.findIndex(m => m.id == id);
        if (index !== -1) {
            this.monitors.splice(index, 1);
            this.saveMonitors();
            // 可选：清理历史文件

            const historyFile = path.join(HISTORY_DIR, `${id}.json`);
            if (fs.existsSync(historyFile)) fs.unlinkSync(historyFile);
            return true;
        }
        return false;
    }

    // ==================== 历史记录处理 ====================

    /**
     * 保存心跳数据
     */
    saveHeartbeat(monitorId, beat) {
        const file = path.join(HISTORY_DIR, `${monitorId}.json`);
        let history = [];

        try {
            if (fs.existsSync(file)) {
                history = JSON.parse(fs.readFileSync(file, 'utf8'));
            }
        } catch (e) { /* ignore */ }

        // 插入新记录到头部
        history.unshift(beat);

        // 默认保留最近 100 条用于显示 (后端可以存储更多，但对于 JSON 实现保持精简)
        if (history.length > 200) history = history.slice(0, 200);

        try {
            fs.writeFileSync(file, JSON.stringify(history), 'utf8');
        } catch (e) {
            logger.error(`Failed to save history for ${monitorId}:`, e);
        }

        return history;
    }

    /**
     * 获取最后一次心跳
     */
    getLastHeartbeat(monitorId) {
        const history = this.getHistory(monitorId, 1);
        return history.length > 0 ? history[0] : null;
    }

    /**
     * 获取历史数据
     */
    getHistory(monitorId, limit = 50) {
        const file = path.join(HISTORY_DIR, `${monitorId}.json`);
        try {
            if (fs.existsSync(file)) {
                const history = JSON.parse(fs.readFileSync(file, 'utf8'));
                return history.slice(0, limit);
            }
        } catch (e) { /* ignore */ }
        return [];
    }
}

module.exports = new UptimeStorage();
