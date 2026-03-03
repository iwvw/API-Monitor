/**
 * Uptime 监控服务
 * 基于状态机驱动的监控与告警
 *
 * 状态机:
 *   OK → (连续 N 次失败) → FIRING → (连续 N 次成功) → OK
 *        不通知                发 Down 通知              发 Up 通知
 *
 * 对比旧实现：
 *   - 旧：每次 Down 都触发通知系统（依赖策略引擎抑制）
 *   - 新：只在状态变迁时触发通知，持续 Down/Up 期间完全静默
 */

const axios = require('axios');
const net = require('net');
const https = require('https');
const fs = require('fs');
const path = require('path');
const storage = require('./storage');
const { createLogger } = require('../../src/utils/logger');

const logger = createLogger('Uptime');

// 全局定时器映射: monitorId -> IntervalID
const intervals = {};
let io = null;

// ==================== 状态机 ====================

const STATE = {
    OK: 'ok',        // 正常运行
    PENDING: 'pending',   // 疑似故障（等待确认）
    FIRING: 'firing',    // 已确认故障（已发 Down 通知）
    RECOVERY: 'recovery',  // 疑似恢复（等待确认）
};

// 默认确认次数
const DEFAULT_CONFIRM_COUNT = 3;

// 状态存储文件
const STATE_FILE = path.join(__dirname, '../../data/uptime-states.json');

// 内存中的状态缓存: monitorId -> { status, failCount, recoveryCount, incidentStart }
let monitorStates = {};

function loadStates() {
    try {
        if (fs.existsSync(STATE_FILE)) {
            monitorStates = JSON.parse(fs.readFileSync(STATE_FILE, 'utf8'));
        }
    } catch (e) {
        logger.warn('加载监控状态失败，使用默认状态');
        monitorStates = {};
    }
}

function saveStates() {
    try {
        fs.writeFileSync(STATE_FILE, JSON.stringify(monitorStates, null, 2), 'utf8');
    } catch (e) {
        logger.error('保存监控状态失败:', e.message);
    }
}

function getState(monitorId) {
    if (!monitorStates[monitorId]) {
        monitorStates[monitorId] = {
            status: STATE.OK,
            failCount: 0,
            recoveryCount: 0,
            incidentStart: null,
        };
    }
    return monitorStates[monitorId];
}

function formatDuration(ms) {
    if (ms < 60000) return `${Math.round(ms / 1000)}秒`;
    if (ms < 3600000) return `${Math.floor(ms / 60000)}分${Math.round((ms % 60000) / 1000)}秒`;
    const h = Math.floor(ms / 3600000);
    const m = Math.floor((ms % 3600000) / 60000);
    return `${h}小时${m}分`;
}

// 启动时加载状态
loadStates();

// ==================== 服务主体 ====================

class UptimeService {
    init(server) {
        this.restartAllMonitors();
        logger.info('Uptime 监控服务已初始化');
    }

    setIO(socketIO) {
        io = socketIO;
    }

    restartAllMonitors() {
        this.stopAll();
        const monitors = storage.getActive();
        monitors.forEach(m => this.startMonitor(m));
        logger.info(`已启动 ${Object.keys(intervals).length} 个监控项`);
    }

    stopAll() {
        Object.values(intervals).forEach(clearInterval);
        for (const key in intervals) delete intervals[key];
    }

    startMonitor(monitor) {
        if (intervals[monitor.id]) clearInterval(intervals[monitor.id]);
        if (!monitor.active) return;

        const seconds = monitor.interval && monitor.interval > 5 ? monitor.interval : 60;

        // 立即执行（延迟 2~4 秒避免启动风暴）
        setTimeout(() => this.check(monitor), 2000 + Math.random() * 2000);

        intervals[monitor.id] = setInterval(() => {
            this.check(monitor);
        }, seconds * 1000);
    }

    stopMonitor(monitorId) {
        if (intervals[monitorId]) {
            clearInterval(intervals[monitorId]);
            delete intervals[monitorId];
        }
    }

    /**
     * 执行检查 + 状态机驱动通知
     */
    async check(monitor) {
        const startTime = Date.now();
        let checkResult = 0; // 0: Down, 1: Up
        let msg = '';
        let ping = 0;

        try {
            if (monitor.type === 'http') {
                await this.checkHttp(monitor);
                checkResult = 1;
                msg = 'OK';
            } else if (monitor.type === 'tcp') {
                await this.checkTcp(monitor);
                checkResult = 1;
                msg = 'OK';
            } else if (monitor.type === 'ping') {
                if (monitor.hostname) {
                    await this.checkPingLike(monitor);
                    checkResult = 1;
                    msg = 'OK';
                } else {
                    throw new Error('Host required');
                }
            } else {
                throw new Error('Unknown Type');
            }
        } catch (error) {
            checkResult = 0;
            msg = error.message;
        }

        ping = checkResult === 1 ? Date.now() - startTime : 0;

        const beat = {
            id: Date.now(),
            status: checkResult,
            msg,
            ping,
            time: new Date().toISOString()
        };

        // 保存心跳
        storage.saveHeartbeat(monitor.id, beat);

        // 状态机处理
        this.processStateMachine(monitor, checkResult, beat);

        // Socket.IO 推送
        if (io) {
            io.emit('uptime:heartbeat', { monitorId: monitor.id, beat });
        }
    }

    /**
     * 状态机核心逻辑
     */
    processStateMachine(monitor, checkResult, beat) {
        const state = getState(monitor.id);
        const confirmCount = monitor.confirmCount || DEFAULT_CONFIRM_COUNT;
        const oldStatus = state.status;

        switch (state.status) {
            case STATE.OK:
                if (checkResult === 0) {
                    state.status = STATE.PENDING;
                    state.failCount = 1;
                    logger.debug(`[${monitor.name}] OK → PENDING (失败 1/${confirmCount})`);
                }
                break;

            case STATE.PENDING:
                if (checkResult === 0) {
                    state.failCount++;
                    logger.debug(`[${monitor.name}] PENDING (失败 ${state.failCount}/${confirmCount})`);

                    if (state.failCount >= confirmCount) {
                        // 确认宕机！
                        state.status = STATE.FIRING;
                        state.incidentStart = Date.now();
                        state.recoveryCount = 0;
                        logger.warn(`[${monitor.name}] PENDING → FIRING: 确认宕机`);

                        // 创建 Incident 记录
                        try { storage.createIncident(monitor.id, beat.msg); } catch (e) { }

                        // ✅ 发送 Down 通知（仅此一次）
                        this.notifyDown(monitor, beat);
                    }
                } else {
                    // 恢复了，是瞬时抖动
                    state.status = STATE.OK;
                    state.failCount = 0;
                    logger.debug(`[${monitor.name}] PENDING → OK: 瞬时抖动，不通知`);
                }
                break;

            case STATE.FIRING:
                if (checkResult === 1) {
                    state.status = STATE.RECOVERY;
                    state.recoveryCount = 1;
                    logger.debug(`[${monitor.name}] FIRING → RECOVERY (恢复 1/${confirmCount})`);
                }
                // 持续 Down → 不做任何事，已经发过通知了
                break;

            case STATE.RECOVERY:
                if (checkResult === 1) {
                    state.recoveryCount++;
                    logger.debug(`[${monitor.name}] RECOVERY (恢复 ${state.recoveryCount}/${confirmCount})`);

                    if (state.recoveryCount >= confirmCount) {
                        // 确认恢复！
                        const duration = Date.now() - (state.incidentStart || Date.now());
                        state.status = STATE.OK;
                        state.failCount = 0;
                        state.recoveryCount = 0;
                        logger.info(`[${monitor.name}] RECOVERY → OK: 确认恢复 (故障持续 ${formatDuration(duration)})`);

                        // 关闭 Incident 记录
                        try { storage.resolveIncident(monitor.id, duration); } catch (e) { }

                        // ✅ 发送 Up 通知（含持续时长）
                        this.notifyUp(monitor, beat, duration);
                        state.incidentStart = null;
                    }
                } else {
                    // 恢复失败，退回 FIRING
                    state.status = STATE.FIRING;
                    state.recoveryCount = 0;
                    logger.debug(`[${monitor.name}] RECOVERY → FIRING: 恢复失败，仍在故障中`);
                }
                break;
        }

        // 状态变化时持久化
        if (oldStatus !== state.status) {
            saveStates();
        }
    }

    /**
     * 发送宕机通知
     */
    notifyDown(monitor, beat) {
        try {
            const notificationService = require('../notification-api/service');
            notificationService.trigger('uptime', 'down', {
                monitorId: monitor.id,
                monitorName: monitor.name,
                url: monitor.url || `${monitor.hostname}:${monitor.port}`,
                error: beat.msg,
                type: monitor.type
            });
        } catch (error) {
            logger.error(`发送宕机通知失败: ${error.message}`);
        }
    }

    /**
     * 发送恢复通知（含故障持续时长）
     */
    notifyUp(monitor, beat, duration) {
        try {
            const notificationService = require('../notification-api/service');
            notificationService.trigger('uptime', 'up', {
                monitorId: monitor.id,
                monitorName: monitor.name,
                url: monitor.url || `${monitor.hostname}:${monitor.port}`,
                ping: beat.ping,
                type: monitor.type,
                downDuration: formatDuration(duration),
                downDurationMs: duration,
            });
        } catch (error) {
            logger.error(`发送恢复通知失败: ${error.message}`);
        }
    }

    /**
     * 获取监控状态（供 API 使用）
     */
    getMonitorState(monitorId) {
        return getState(monitorId);
    }

    getAllMonitorStates() {
        return { ...monitorStates };
    }

    // ==================== 检查逻辑 ====================

    async checkHttp(monitor) {
        const agent = new https.Agent({
            rejectUnauthorized: !monitor.ignoreTls
        });

        const config = {
            url: monitor.url,
            method: monitor.method || 'GET',
            timeout: (monitor.timeout || 30) * 1000,
            headers: monitor.headers ? JSON.parse(monitor.headers) : {},
            httpsAgent: agent,
            validateStatus: function (status) {
                return status >= 200 && status < 300;
            }
        };

        if (monitor.accepted_status_codes) {
            config.validateStatus = () => true;
        }

        const res = await axios(config);
        return res;
    }

    checkTcp(monitor) {
        return new Promise((resolve, reject) => {
            const socket = new net.Socket();
            socket.setTimeout((monitor.timeout || 10) * 1000);

            socket.on('connect', () => {
                socket.destroy();
                resolve();
            });

            socket.on('timeout', () => {
                socket.destroy();
                reject(new Error('Connection Timeout'));
            });

            socket.on('error', (err) => {
                socket.destroy();
                reject(err);
            });

            socket.connect(monitor.port, monitor.hostname);
        });
    }

    async checkPingLike(monitor) {
        const ports = [80, 443, 53];
        for (const p of ports) {
            try {
                await this.checkTcp({ hostname: monitor.hostname, port: p, timeout: 2 });
                return;
            } catch (e) { }
        }
        throw new Error('Ping(Tcp) Failed');
    }
}

module.exports = new UptimeService();
