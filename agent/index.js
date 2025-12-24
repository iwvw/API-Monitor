/**
 * API Monitor Agent - 入口文件
 * 基于 Socket.IO 的实时监控 Agent
 */

const { io } = require('socket.io-client');
const { loadConfig } = require('./config');
const { collectHostInfo, collectState, getCachedHostInfo } = require('./collector');

// 版本信息
const VERSION = require('./package.json').version;

// 事件类型 (与服务端 protocol.js 保持一致)
const Events = {
    AGENT_CONNECT: 'agent:connect',
    AGENT_HOST_INFO: 'agent:host_info',
    AGENT_STATE: 'agent:state',
    AGENT_TASK_RESULT: 'agent:task_result',
    DASHBOARD_AUTH_OK: 'dashboard:auth_ok',
    DASHBOARD_AUTH_FAIL: 'dashboard:auth_fail',
    DASHBOARD_TASK: 'dashboard:task'
};

class AgentClient {
    constructor(config) {
        this.config = config;
        this.socket = null;
        this.authenticated = false;
        this.reportTimer = null;
        this.hostInfoTimer = null;
        this.reconnectCount = 0;
    }

    /**
     * 启动 Agent
     */
    async start() {
        console.log('═══════════════════════════════════════════════');
        console.log(`  API Monitor Agent v${VERSION}`);
        console.log('═══════════════════════════════════════════════');
        console.log(`  Server:   ${this.config.serverUrl}`);
        console.log(`  ServerID: ${this.config.serverId}`);
        console.log(`  Interval: ${this.config.reportInterval}ms`);
        console.log('═══════════════════════════════════════════════');

        // 立即开始预热数据采集（不阻塞连接）
        console.log('[Agent] 正在预热数据采集...');

        // 同时预热主机信息和实时状态采集
        Promise.all([
            collectHostInfo().then(() => console.log('[Agent] ✓ 主机信息预热完成')),
            collectState().then(() => console.log('[Agent] ✓ 实时状态预热完成'))
        ]).catch(err => console.warn('[Agent] 预热采集出错:', err.message));

        // 不等待预热完成，直接连接服务器
        this.connect();
    }

    /**
     * 连接到 Dashboard
     */
    connect() {
        const url = `${this.config.serverUrl}/agent`;
        console.log(`[Agent] 正在连接: ${url}`);

        this.socket = io(url, {
            reconnection: true,
            reconnectionDelay: this.config.reconnectInterval,
            reconnectionDelayMax: 30000,
            reconnectionAttempts: Infinity,
            transports: ['websocket', 'polling']
        });

        // 连接成功
        this.socket.on('connect', () => {
            console.log('[Agent] 已连接，正在认证...');
            this.reconnectCount = 0;
            this.authenticate();
        });

        // 认证成功
        this.socket.on(Events.DASHBOARD_AUTH_OK, (data) => {
            console.log('[Agent] ✅ 认证成功');
            this.authenticated = true;

            // 发送主机信息
            this.reportHostInfo();

            // 启动定时上报
            this.startReportLoop();
        });

        // 认证失败
        this.socket.on(Events.DASHBOARD_AUTH_FAIL, (data) => {
            console.error(`[Agent] ❌ 认证失败: ${data.reason}`);
            this.authenticated = false;
            // 认证失败不自动重连，直接退出
            process.exit(1);
        });

        // 接收任务
        this.socket.on(Events.DASHBOARD_TASK, (task) => {
            this.handleTask(task);
        });

        // 断开连接
        this.socket.on('disconnect', (reason) => {
            console.log(`[Agent] 连接断开: ${reason}`);
            this.authenticated = false;
            this.stopReportLoop();
        });

        // 重连中
        this.socket.on('reconnect_attempt', (attempt) => {
            this.reconnectCount = attempt;
            console.log(`[Agent] 正在重连... (第 ${attempt} 次)`);
        });

        // 重连成功
        this.socket.on('reconnect', () => {
            console.log('[Agent] 重连成功');
            // 重新认证
            this.authenticate();
        });

        // 错误处理
        this.socket.on('connect_error', (err) => {
            if (this.config.debug) {
                console.error('[Agent] 连接错误:', err.message);
            }
        });

        this.socket.on('error', (err) => {
            console.error('[Agent] Socket 错误:', err.message);
        });
    }

    /**
     * 发送认证请求
     */
    authenticate() {
        this.socket.emit(Events.AGENT_CONNECT, {
            server_id: this.config.serverId,
            key: this.config.agentKey,
            hostname: require('os').hostname(),
            version: VERSION
        });
    }

    /**
     * 上报主机信息
     */
    async reportHostInfo() {
        try {
            const hostInfo = await collectHostInfo();
            this.socket.emit(Events.AGENT_HOST_INFO, hostInfo);

            if (this.config.debug) {
                console.log('[Agent] 已上报主机信息');
            }
        } catch (err) {
            console.error('[Agent] 上报主机信息失败:', err.message);
        }
    }

    /**
     * 上报实时状态
     */
    async reportState() {
        if (!this.authenticated) {
            console.log('[Agent] 跳过状态上报: 未认证');
            return;
        }

        try {
            console.log('[Agent] 正在采集状态...');
            const state = await collectState();
            console.log('[Agent] 采集完成，正在发送...');
            this.socket.emit(Events.AGENT_STATE, state);

            if (this.config.debug) {
                console.log(`[Agent] 状态上报: CPU=${state.cpu.toFixed(1)}%, MEM=${(state.mem_used / 1024 / 1024 / 1024).toFixed(1)}GB`);
            }
        } catch (err) {
            console.error('[Agent] 状态上报失败:', err.message);
        }
    }

    /**
     * 启动定时上报循环
     */
    startReportLoop() {
        this.stopReportLoop();

        // 立即上报一次
        this.reportState();

        // 状态上报定时器
        this.reportTimer = setInterval(() => {
            this.reportState();
        }, this.config.reportInterval);

        // 主机信息定时器 (每 10 分钟)
        this.hostInfoTimer = setInterval(() => {
            this.reportHostInfo();
        }, this.config.hostInfoInterval || 600000);

        console.log(`[Agent] 上报循环已启动 (间隔: ${this.config.reportInterval}ms)`);
    }

    /**
     * 停止定时上报
     */
    stopReportLoop() {
        if (this.reportTimer) {
            clearInterval(this.reportTimer);
            this.reportTimer = null;
        }
        if (this.hostInfoTimer) {
            clearInterval(this.hostInfoTimer);
            this.hostInfoTimer = null;
        }
    }

    /**
     * 处理任务
     */
    async handleTask(task) {
        console.log(`[Agent] 收到任务: ${task.id} (type=${task.type})`);

        const result = {
            id: task.id,
            type: task.type,
            successful: false,
            data: '',
            delay: 0
        };

        const startTime = Date.now();

        try {
            switch (task.type) {
                case 1: // COMMAND
                    result.data = await this.executeCommand(task.data, task.timeout);
                    result.successful = true;
                    break;

                case 6: // REPORT_HOST_INFO
                    await this.reportHostInfo();
                    result.successful = true;
                    break;

                case 7: // KEEPALIVE
                    result.successful = true;
                    break;

                default:
                    result.data = `不支持的任务类型: ${task.type}`;
            }
        } catch (err) {
            result.data = err.message;
        }

        result.delay = Date.now() - startTime;

        // 上报结果
        this.socket.emit(Events.AGENT_TASK_RESULT, result);
        console.log(`[Agent] 任务完成: ${task.id} (${result.successful ? '成功' : '失败'})`);
    }

    /**
     * 执行命令
     */
    executeCommand(command, timeout = 60000) {
        return new Promise((resolve, reject) => {
            const { exec } = require('child_process');

            exec(command, {
                timeout: timeout || 60000,
                maxBuffer: 10 * 1024 * 1024
            }, (error, stdout, stderr) => {
                if (error) {
                    reject(new Error(stderr || error.message));
                } else {
                    resolve(stdout);
                }
            });
        });
    }

    /**
     * 关闭连接
     */
    close() {
        this.stopReportLoop();
        if (this.socket) {
            this.socket.disconnect();
            this.socket = null;
        }
        console.log('[Agent] 已关闭');
    }
}

// ==================== 主程序 ====================

async function main() {
    const config = loadConfig();

    const agent = new AgentClient(config);

    // 优雅退出
    process.on('SIGINT', () => {
        console.log('\n[Agent] 收到退出信号...');
        agent.close();
        process.exit(0);
    });

    process.on('SIGTERM', () => {
        console.log('[Agent] 收到终止信号...');
        agent.close();
        process.exit(0);
    });

    // 启动
    await agent.start();
}

main().catch(err => {
    console.error('[Agent] 启动失败:', err);
    process.exit(1);
});
