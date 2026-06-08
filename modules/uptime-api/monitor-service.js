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
const probeRegistry = require('./adapters/probe-registry');
const { STATES, transition } = require('./domain/state-machine');
const { createLogger } = require('../../src/utils/logger');
const eventBus = require('../../src/services/toolbox-event-bus');

const logger = createLogger('Uptime');

// 全局定时器映射: monitorId -> IntervalID
const intervals = {};
let io = null;

// ==================== 状态机 ====================

const STATE = {
    OK: STATES.UP,
    PENDING: STATES.PENDING_DOWN,
    FIRING: STATES.DOWN,
    RECOVERY: STATES.PENDING_UP,
};

// 默认确认次数
const DEFAULT_CONFIRM_COUNT = 3;

// 状态存储文件
const STATE_FILE = path.join(__dirname, '../../data/uptime-states.json');

// 内存中的状态缓存: monitorId -> { status, failCount, recoveryCount, incidentStart }
let monitorStates = {};

function loadStates() {
    try {
        const persistedStates = storage.getAllStates();
        if (Object.keys(persistedStates).length > 0) {
            monitorStates = persistedStates;
            return;
        }

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
        for (const [monitorId, state] of Object.entries(monitorStates)) {
            storage.saveMonitorState(monitorId, state);
        }
    } catch (e) {
        logger.error('保存监控状态失败:', e.message);
    }
}

function getState(monitorId) {
    if (!monitorStates[monitorId]) {
        monitorStates[monitorId] = {
            status: STATE.OK,
            state: STATES.UP,
            failCount: 0,
            recoveryCount: 0,
            recoverCount: 0,
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
        let result;

        try {
            result = await probeRegistry.check(monitor);
        } catch (error) {
            result = {
                ok: false,
                status: 'down',
                latencyMs: 0,
                message: error.message,
                errorCode: error.code || error.name || 'CHECK_FAILED',
            };
        }

        const beat = {
            id: Date.now(),
            status: result.ok ? 1 : 0,
            state: result.status || (result.ok ? 'up' : 'down'),
            msg: result.message || (result.ok ? 'OK' : 'Failed'),
            ping: result.ok ? result.latencyMs || 0 : 0,
            durationMs: result.latencyMs || 0,
            statusCode: result.statusCode,
            errorCode: result.errorCode,
            details: result.details,
            time: new Date().toISOString(),
        };

        const maintenance = storage.getActiveMaintenanceForMonitor(monitor.id);
        beat.maintenance = !!maintenance;

        // 保存心跳
        storage.saveHeartbeat(monitor.id, beat);

        // 状态机处理
        this.processStateMachine(monitor, result, beat, maintenance);

        // Socket.IO 推送
        if (io) {
            io.emit('uptime:heartbeat', { monitorId: monitor.id, beat });
        }
        return beat;
    }

    recordPush(token, payload = {}, req = null) {
        const monitor = storage.getByPushToken(token);
        if (!monitor) return null;

        const beat = {
            id: Date.now(),
            status: 1,
            state: 'up',
            msg: 'Push heartbeat received',
            ping: 0,
            durationMs: 0,
            details: {
                payload,
                ip: req?.ip || req?.socket?.remoteAddress || null,
                userAgent: req?.get ? req.get('user-agent') : null,
            },
            probeId: 'push',
            time: new Date().toISOString(),
        };

        const maintenance = storage.getActiveMaintenanceForMonitor(monitor.id);
        beat.maintenance = !!maintenance;
        storage.saveHeartbeat(monitor.id, beat);
        this.processStateMachine(monitor, { ok: true, status: 'up', latencyMs: 0, message: beat.msg }, beat, maintenance);

        if (io) {
            io.emit('uptime:heartbeat', { monitorId: monitor.id, beat });
        }

        return { monitor, beat };
    }

    /**
     * 状态机核心逻辑
     */
    processStateMachine(monitor, checkResult, beat, maintenanceOverride = undefined) {
        const state = getState(monitor.id);
        const oldState = state.state || state.status;
        const maintenance = maintenanceOverride === undefined
            ? storage.getActiveMaintenanceForMonitor(monitor.id)
            : maintenanceOverride;
        const { nextState, incidentAction, notificationAction } = transition(
            state,
            checkResult,
            {
                ...monitor,
                downConfirmCount: monitor.downConfirmCount || monitor.confirmCount || DEFAULT_CONFIRM_COUNT,
                upConfirmCount: monitor.upConfirmCount || monitor.confirmCount || DEFAULT_CONFIRM_COUNT,
            },
            { active: !!maintenance, maintenance }
        );

        if (incidentAction?.type === 'open') {
            try {
                const incidentId = storage.createIncident(monitor.id, beat.msg);
                nextState.activeIncidentId = incidentId;
            } catch (e) { }
            this.notifyDown(monitor, beat);
        }

        if (incidentAction?.type === 'resolve') {
            const openIncident = storage.getOpenIncident(monitor.id);
            const startedAt = state.incidentStart || openIncident?.started_at;
            const duration = startedAt ? Math.max(0, Date.now() - new Date(startedAt).getTime()) : 0;
            try { storage.resolveIncident(monitor.id, duration); } catch (e) { }
            this.notifyUp(monitor, beat, duration);
            nextState.activeIncidentId = null;
        }

        monitorStates[monitor.id] = {
            ...nextState,
            status: nextState.state,
            recoveryCount: nextState.recoverCount,
        };
        storage.saveMonitorState(monitor.id, monitorStates[monitor.id]);

        if (oldState !== nextState.state) {
            logger.info(`[${monitor.name}] 状态变更: ${oldState} -> ${nextState.state}`);
            eventBus.publish('uptime.monitor.state', {
                monitorId: monitor.id,
                from: oldState,
                to: nextState.state,
                notificationAction,
            }, { module: 'uptime', severity: nextState.state === STATES.DOWN ? 'warning' : 'info' });
            if (io) {
                io.emit('uptime:monitor-state', {
                    monitorId: monitor.id,
                    from: oldState,
                    to: nextState.state,
                    state: monitorStates[monitor.id],
                });
            }
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
