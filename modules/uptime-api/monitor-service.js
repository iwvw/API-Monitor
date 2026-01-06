/**
 * Uptime 监控服务
 * 处理实际的检查逻辑和调度
 */

const axios = require('axios');
const net = require('net');
const https = require('https');
const storage = require('./storage');
const { createLogger } = require('../../src/utils/logger');

const logger = createLogger('Uptime');

// 全局定时器映射: monitorId -> IntervalID
const intervals = {};
let io = null;

class UptimeService {
    /**
     * 使用 Server 初始化以获取 Socket.IO
     */
    init(server) {
        // 如果 Server.js 中已经附加了 Socket.IO (通常是这样的)。
        // 我们假设 `monitor-service` 在 router 或 server.js 中被引用，并且可以传递 IO。

        // 目前先重启所有监控项
        this.restartAllMonitors();
        logger.info('Uptime 监控服务已初始化');
    }

    setIO(socketIO) {
        io = socketIO;
    }

    /**
     * 重启所有活跃的监控项 (例如启动时)
     */
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

    /**
     * 启动单个监控项
     */
    startMonitor(monitor) {
        if (intervals[monitor.id]) clearInterval(intervals[monitor.id]);
        if (!monitor.active) return;

        // 默认间隔 60秒
        const seconds = monitor.interval && monitor.interval > 5 ? monitor.interval : 60;

        // 立即执行初步检查 (稍微延迟以避免启动风暴)
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
     * 执行检查
     */
    async check(monitor) {
        const startTime = Date.now();
        let status = 0; // 0: Down, 1: Up
        let msg = '';
        let ping = 0;

        try {
            if (monitor.type === 'http') {
                await this.checkHttp(monitor);
                status = 1;
                msg = 'OK';
            } else if (monitor.type === 'tcp') {
                await this.checkTcp(monitor);
                status = 1;
                msg = 'OK';
            } else if (monitor.type === 'ping') {
                // 如果没有通用的 ping 库，回退到 TCP ping
                if (monitor.hostname) {
                    // 基础变通方案: 如果未指定端口，尝试连接 80 或 443 端口。
                    // 'ping' 通常指 ICMP，但由于权限问题，这里如果用户输入主机名，
                    // 我们实现基础的 TCP 连接到 80/443 作为 "ping" 类型的替代。
                    // 真正的 ICMP 通常需要特权执行。
                    await this.checkPingLike(monitor);
                    status = 1;
                    msg = 'OK';
                } else {
                    throw new Error('Host required');
                }
            } else {
                throw new Error('Unknown Type');
            }
        } catch (error) {
            status = 0;
            msg = error.message;
            // logger.debug(`Check failed for ${monitor.name}: ${error.message}`);
        }

        if (status === 1) {
            ping = Date.now() - startTime;
        } else {
            ping = 0;
        }

        const beat = {
            id: Date.now(),
            status,
            msg,
            ping,
            time: new Date().toISOString()
        };

        // 保存
        storage.saveHeartbeat(monitor.id, beat);

        // 通过 Socket.IO 推送
        if (io) {
            io.emit('uptime:heartbeat', { monitorId: monitor.id, beat });
        }
    }

    // --- 检查逻辑 ---

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
                // Parse accepted codes e.g. "200-299"
                // Simple impl: return true, we check result later or let axios throw if outside 2xx? 
                // Axios throws for <200 || >=300 by default.
                return status >= 200 && status < 300;
            }
        };

        // 如果用户指定了状态码，我们需要自定义验证器
        if (monitor.accepted_status_codes) {
            config.validateStatus = (status) => {
                // "200-299" -> 200..299
                // "200, 201"
                // TODO: 增强健壮性。目前假设默认范围行为或简单匹配。
                return true; // 我们将在下面手动检查，或者如果返回就让它通过
            };
        }

        const res = await axios(config);

        // 如果需要，在此处显式检查状态码逻辑
        if (monitor.accepted_status_codes) {
            // 最简单的情况: 默认 200-299
            // 如果失败则抛出错误
        }
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
        // 使用 TCP 连接到 80, 443 或 53 作为仅有主机名时的 "Ping" 代理
        // 这是一个简单的近似实现
        const ports = [80, 443, 53];
        for (const p of ports) {
            try {
                await this.checkTcp({ hostname: monitor.hostname, port: p, timeout: 2 });
                return; // Success one is enough
            } catch (e) { }
        }
        throw new Error('Ping(Tcp) Failed');
    }
}

module.exports = new UptimeService();
