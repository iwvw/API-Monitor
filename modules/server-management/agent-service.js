/**
 * Agent 服务 - 基于 Socket.IO 的实时连接管理器
 * 参考 Nezha 0.20.13 架构设计
 */

const crypto = require('crypto');
const fs = require('fs');
const path = require('path');
const { Server: SocketIOServer } = require('socket.io');
const { serverStorage } = require('./storage');
const {
    Events,
    TaskTypes,
    validateHostState,
    stateToFrontendFormat
} = require('./protocol');
const { ServerMetricsHistory, ServerMonitorConfig } = require('./models');

class AgentService {
    constructor() {
        // 全局统一 Agent 密钥
        this.globalAgentKey = null;
        // 密钥存储路径
        this.keyFilePath = path.join(__dirname, '../../data/agent-key.txt');

        // Socket.IO 服务端实例
        this.io = null;

        // 连接池: serverId -> socket
        this.connections = new Map();

        // 主机信息缓存: serverId -> HostInfo
        this.hostInfoCache = new Map();

        // 实时状态缓存: serverId -> { state, timestamp }
        this.stateCache = new Map();

        // 心跳超时定时器: serverId -> timerId
        this.heartbeatTimers = new Map();

        // 心跳超时时间 (毫秒) - 增加到 30 秒以适应采集延迟
        this.heartbeatTimeout = 30000;

        // 兼容旧版 HTTP 推送的缓存 (过渡期使用)
        this.legacyMetrics = new Map();
        this.legacyStatus = new Map();

        // 初始化加载或生成全局密钥
        this.loadOrGenerateGlobalKey();
    }

    /**
     * 加载或生成全局 Agent 密钥
     */
    loadOrGenerateGlobalKey() {
        try {
            const dataDir = path.dirname(this.keyFilePath);
            if (!fs.existsSync(dataDir)) {
                fs.mkdirSync(dataDir, { recursive: true });
            }

            if (fs.existsSync(this.keyFilePath)) {
                this.globalAgentKey = fs.readFileSync(this.keyFilePath, 'utf8').trim();
                console.log('[AgentService] 已加载全局 Agent 密钥');
            } else {
                this.globalAgentKey = crypto.randomBytes(16).toString('hex');
                fs.writeFileSync(this.keyFilePath, this.globalAgentKey);
                console.log('[AgentService] 已生成新的全局 Agent 密钥');
            }
        } catch (error) {
            console.error('[AgentService] 密钥管理失败:', error.message);
            this.globalAgentKey = crypto.randomBytes(16).toString('hex');
        }
    }

    /**
     * 获取全局 Agent 密钥
     */
    getAgentKey(serverId) {
        return this.globalAgentKey;
    }

    /**
     * 重新生成全局密钥
     */
    regenerateGlobalKey() {
        this.globalAgentKey = crypto.randomBytes(16).toString('hex');
        try {
            fs.writeFileSync(this.keyFilePath, this.globalAgentKey);
        } catch (e) {
            console.error('[AgentService] 保存密钥失败:', e.message);
        }
        return this.globalAgentKey;
    }

    /**
     * 验证 Agent 请求 (兼容性方法)
     */
    verifyAgent(serverId, providedKey) {
        return providedKey === this.globalAgentKey;
    }

    /**
     * 获取当前连接的 Agent 数量
     */
    getConnectionCount() {
        return this.connections.size;
    }

    // ==================== Socket.IO 服务 ====================

    /**
     * 初始化 Socket.IO 服务
     * @param {Object} httpServer - HTTP 服务器实例
     */
    initSocketIO(httpServer) {
        this.io = new SocketIOServer(httpServer, {
            cors: {
                origin: '*',
                methods: ['GET', 'POST']
            },
            pingTimeout: 10000,
            pingInterval: 5000
        });

        // Agent 命名空间 - 处理 Agent 连接
        const agentNamespace = this.io.of('/agent');
        agentNamespace.on('connection', (socket) => this.handleAgentConnection(socket));

        // Metrics 命名空间 - 处理前端订阅
        const metricsNamespace = this.io.of('/metrics');
        metricsNamespace.on('connection', (socket) => this.handleFrontendConnection(socket));

        // 启动历史指标自动采集定时器
        this.startHistoryCollector();

        console.log('[AgentService] Socket.IO 已初始化 (命名空间: /agent, /metrics)');
    }

    /**
     * 启动历史指标自动采集
     */
    startHistoryCollector() {
        // 如果已存在定时器，先清除
        if (this.historyCollectorTimer) {
            clearInterval(this.historyCollectorTimer);
        }

        // 获取采集间隔 (优先从配置读取，默认 300 秒)
        const config = ServerMonitorConfig.get();
        const intervalSec = config?.metrics_collect_interval || 300;
        const intervalMs = intervalSec * 1000;

        console.log(`[AgentService] 历史指标自动采集已启动 (间隔: ${intervalSec}秒)`);

        // 立即执行一次采集
        this.collectHistoryMetrics();

        // 设置定时采集
        this.historyCollectorTimer = setInterval(() => {
            this.collectHistoryMetrics();
        }, intervalMs);
    }

    /**
     * 采集当前所有在线主机的指标并存入历史记录
     */
    collectHistoryMetrics() {
        try {
            let collected = 0;
            const servers = serverStorage.getAll();

            for (const server of servers) {
                const cached = this.stateCache.get(server.id);
                if (!cached) continue;

                const hostInfo = this.hostInfoCache.get(server.id) || {};
                const state = cached.state;

                // 使用协议转换函数获取前端格式指标
                const frontendMetrics = stateToFrontendFormat(state, hostInfo);

                // 解析内存数值 (格式: "123/456MB")
                let memUsed = 0;
                let memTotal = 0;
                if (frontendMetrics.mem && typeof frontendMetrics.mem === 'string') {
                    const parts = frontendMetrics.mem.replace('MB', '').split('/');
                    memUsed = parseInt(parts[0]) || 0;
                    memTotal = parseInt(parts[1]) || 0;
                }

                ServerMetricsHistory.create({
                    server_id: server.id,
                    cpu_usage: parseFloat(frontendMetrics.cpu_usage) || 0,
                    cpu_load: frontendMetrics.load || '',
                    cpu_cores: frontendMetrics.cores || 1,
                    mem_used: memUsed,
                    mem_total: memTotal,
                    mem_usage: frontendMetrics.mem_percent || 0,
                    disk_used: frontendMetrics.disk_used || '',
                    disk_total: frontendMetrics.disk_total || '',
                    disk_usage: frontendMetrics.disk_percent || 0,
                    docker_installed: frontendMetrics.docker?.installed ? 1 : 0,
                    docker_running: frontendMetrics.docker?.running || 0,
                    docker_stopped: frontendMetrics.docker?.stopped || 0
                });
                collected++;
            }

            if (collected > 0) {
                console.log(`[AgentService] 历史指标采集完成: ${collected} 台主机`);
            }
        } catch (error) {
            console.error('[AgentService] 历史指标采集失败:', error.message);
        }
    }

    /**
     * 处理 Agent 连接
     * @param {Object} socket - Socket.IO 连接
     */
    handleAgentConnection(socket) {
        let serverId = null;
        let authenticated = false;

        console.log(`[AgentService] Agent 连接中: ${socket.id}`);

        // 设置认证超时 (10 秒内必须完成认证)
        const authTimeout = setTimeout(() => {
            if (!authenticated) {
                console.log(`[AgentService] Agent 认证超时: ${socket.id}`);
                socket.emit(Events.DASHBOARD_AUTH_FAIL, { reason: 'Authentication timeout' });
                socket.disconnect();
            }
        }, 10000);

        // 1. 处理认证请求
        socket.on(Events.AGENT_CONNECT, (data) => {
            clearTimeout(authTimeout);

            // 验证密钥
            if (!data || data.key !== this.globalAgentKey) {
                console.log(`[AgentService] Agent 认证失败: 无效密钥`);
                socket.emit(Events.DASHBOARD_AUTH_FAIL, { reason: 'Invalid key' });
                socket.disconnect();
                return;
            }

            // 解析 server_id 和 hostname
            const requestedId = data.server_id;
            const hostname = data.hostname;

            if (!requestedId && !hostname) {
                socket.emit(Events.DASHBOARD_AUTH_FAIL, { reason: 'Missing server_id or hostname' });
                socket.disconnect();
                return;
            }

            // 智能匹配主机 ID
            serverId = this.resolveServerId(requestedId, hostname);

            if (!serverId) {
                console.log(`[AgentService] Agent 认证失败: 无法匹配主机 (id=${requestedId}, hostname=${hostname})`);
                socket.emit(Events.DASHBOARD_AUTH_FAIL, {
                    reason: 'Server not found in dashboard. Please add the host first.',
                    requested_id: requestedId,
                    hostname: hostname
                });
                socket.disconnect();
                return;
            }

            // 检查是否有旧连接，断开它
            const oldSocket = this.connections.get(serverId);
            if (oldSocket && oldSocket.id !== socket.id) {
                console.log(`[AgentService] 断开旧连接: ${serverId}`);
                oldSocket.disconnect();
            }

            // 注册新连接
            authenticated = true;
            this.connections.set(serverId, socket);
            this.startHeartbeat(serverId);

            // 更新数据库状态
            this.updateServerStatus(serverId, 'online');

            // 发送认证成功 (包含解析后的实际 serverId)
            socket.emit(Events.DASHBOARD_AUTH_OK, {
                server_time: Date.now(),
                heartbeat_interval: this.heartbeatTimeout / 2,
                resolved_id: serverId  // 告知 Agent 实际使用的 ID
            });

            // 广播上线状态给前端
            this.broadcastServerStatus(serverId, 'online');

            console.log(`[AgentService] Agent 认证成功: ${serverId} (requested: ${requestedId}, hostname: ${hostname}, version: ${data.version || 'unknown'})`);
        });

        // 2. 接收主机硬件信息
        socket.on(Events.AGENT_HOST_INFO, (hostInfo) => {
            if (!authenticated) return;

            this.hostInfoCache.set(serverId, {
                ...hostInfo,
                received_at: Date.now()
            });

            console.log(`[AgentService] 收到主机信息: ${serverId} (${hostInfo.platform} ${hostInfo.platform_version})`);
        });

        // 3. 接收实时状态
        socket.on(Events.AGENT_STATE, (state) => {
            if (!authenticated) {
                console.warn(`[AgentService] 收到未认证 Agent 的状态数据，忽略`);
                return;
            }

            // 验证数据
            if (!validateHostState(state)) {
                console.warn(`[AgentService] 无效状态数据: ${serverId}`, JSON.stringify(state).substring(0, 200));
                return;
            }

            // 存储状态
            const timestamp = Date.now();
            this.stateCache.set(serverId, {
                state,
                timestamp
            });

            // 重置心跳 - 在此行添加日志确认执行
            console.log(`[AgentService] 收到状态上报: ${serverId} CPU=${state.cpu?.toFixed(1)}%`);
            this.resetHeartbeat(serverId);

            // 转换为前端格式并广播
            const hostInfo = this.hostInfoCache.get(serverId) || {};
            const frontendData = stateToFrontendFormat(state, hostInfo);

            this.broadcastMetrics(serverId, frontendData);

            // 同时更新兼容缓存
            this.legacyMetrics.set(serverId, frontendData);
            this.legacyStatus.set(serverId, {
                lastSeen: timestamp,
                connected: true,
                version: hostInfo.agent_version || 'socket.io'
            });
        });

        // 4. 接收任务结果
        socket.on(Events.AGENT_TASK_RESULT, (result) => {
            if (!authenticated) return;
            console.log(`[AgentService] 任务结果: ${serverId} -> ${result.id} (${result.successful ? '成功' : '失败'})`);
            // TODO: 处理任务结果 (日志记录、通知等)
        });

        // 5. 断开连接
        socket.on('disconnect', (reason) => {
            if (serverId) {
                console.log(`[AgentService] Agent 断开: ${serverId} (${reason})`);
                this.connections.delete(serverId);
                this.stopHeartbeat(serverId);
                this.updateServerStatus(serverId, 'offline');
                this.broadcastServerStatus(serverId, 'offline');

                // 更新兼容缓存
                const status = this.legacyStatus.get(serverId);
                if (status) {
                    status.connected = false;
                }
            }
        });

        // 错误处理
        socket.on('error', (err) => {
            console.error(`[AgentService] Socket 错误 (${serverId || socket.id}):`, err.message);
        });
    }

    /**
     * 处理前端连接
     * @param {Object} socket - Socket.IO 连接
     */
    handleFrontendConnection(socket) {
        // 自动加入广播房间
        socket.join('metrics_room');
        console.log(`[AgentService] 前端连接: ${socket.id}`);

        // 发送当前所有在线主机的最新状态
        const initialData = [];
        for (const [serverId, cached] of this.stateCache.entries()) {
            const hostInfo = this.hostInfoCache.get(serverId) || {};
            initialData.push({
                serverId,
                metrics: stateToFrontendFormat(cached.state, hostInfo),
                timestamp: cached.timestamp
            });
        }

        if (initialData.length > 0) {
            socket.emit(Events.METRICS_BATCH, initialData);
        }

        // 发送所有在线主机的状态 (确保前端知道哪些主机在线)
        for (const [serverId] of this.connections.entries()) {
            socket.emit(Events.SERVER_STATUS, {
                serverId,
                status: 'online',
                timestamp: Date.now()
            });
        }

        socket.on('disconnect', () => {
            console.log(`[AgentService] 前端断开: ${socket.id}`);
        });
    }

    // ==================== 心跳管理 ====================

    /**
     * 启动心跳超时检测
     */
    startHeartbeat(serverId) {
        this.stopHeartbeat(serverId);
        this.heartbeatTimers.set(serverId, setTimeout(() => {
            console.log(`[AgentService] 心跳超时: ${serverId}`);
            const socket = this.connections.get(serverId);
            if (socket) {
                socket.disconnect();
            }
            this.handleAgentTimeout(serverId);
        }, this.heartbeatTimeout));
    }

    /**
     * 重置心跳计时器
     */
    resetHeartbeat(serverId) {
        this.startHeartbeat(serverId);
    }

    /**
     * 停止心跳检测
     */
    stopHeartbeat(serverId) {
        const timer = this.heartbeatTimers.get(serverId);
        if (timer) {
            clearTimeout(timer);
            this.heartbeatTimers.delete(serverId);
        }
    }

    /**
     * 处理 Agent 超时
     */
    handleAgentTimeout(serverId) {
        this.connections.delete(serverId);
        this.updateServerStatus(serverId, 'offline');
        this.broadcastServerStatus(serverId, 'offline');
    }

    // ==================== 广播方法 ====================

    /**
     * 广播单个主机的指标更新
     */
    broadcastMetrics(serverId, metrics) {
        if (!this.io) return;

        this.io.of('/metrics').to('metrics_room').emit(Events.METRICS_UPDATE, {
            serverId,
            metrics,
            timestamp: Date.now()
        });
    }

    /**
     * 广播主机状态变更
     */
    broadcastServerStatus(serverId, status) {
        if (!this.io) return;

        this.io.of('/metrics').to('metrics_room').emit(Events.SERVER_STATUS, {
            serverId,
            status,
            timestamp: Date.now()
        });
    }

    // ==================== 主机匹配 ====================

    /**
     * 智能解析 Agent 提供的标识符，匹配到数据库中的主机 ID
     * 匹配优先级: 精确 ID -> 名称匹配 -> 主机地址匹配
     * @param {string} requestedId - Agent 请求的 ID
     * @param {string} hostname - Agent 的 hostname
     * @returns {string|null} 匹配到的主机 ID，未匹配返回 null
     */
    resolveServerId(requestedId, hostname) {
        try {
            const servers = serverStorage.getAll();

            // 1. 精确 ID 匹配
            if (requestedId) {
                const exactMatch = servers.find(s => s.id === requestedId);
                if (exactMatch) {
                    return exactMatch.id;
                }
            }

            // 2. 按名称匹配 (requestedId 或 hostname 与主机名称匹配)
            const nameToMatch = requestedId || hostname;
            if (nameToMatch) {
                // 精确名称匹配
                const nameMatch = servers.find(s =>
                    s.name === nameToMatch ||
                    s.name?.toLowerCase() === nameToMatch.toLowerCase()
                );
                if (nameMatch) {
                    console.log(`[AgentService] 按名称匹配: ${nameToMatch} -> ${nameMatch.id}`);
                    return nameMatch.id;
                }
            }

            // 3. 按主机地址匹配 (hostname 与 host 字段匹配)
            if (hostname) {
                const hostMatch = servers.find(s =>
                    s.host === hostname ||
                    s.host?.toLowerCase() === hostname.toLowerCase()
                );
                if (hostMatch) {
                    console.log(`[AgentService] 按 host 匹配: ${hostname} -> ${hostMatch.id}`);
                    return hostMatch.id;
                }
            }

            // 4. 部分名称匹配 (模糊匹配)
            if (nameToMatch) {
                const partialMatch = servers.find(s =>
                    s.name?.toLowerCase().includes(nameToMatch.toLowerCase()) ||
                    nameToMatch.toLowerCase().includes(s.name?.toLowerCase())
                );
                if (partialMatch) {
                    console.log(`[AgentService] 模糊名称匹配: ${nameToMatch} -> ${partialMatch.id}`);
                    return partialMatch.id;
                }
            }

            return null;
        } catch (error) {
            console.error('[AgentService] 主机匹配失败:', error.message);
            return null;
        }
    }

    // ==================== 任务下发 ====================

    /**
     * 向 Agent 下发任务
     * @param {string} serverId - 目标主机 ID
     * @param {Object} task - 任务对象
     * @returns {boolean} 是否成功发送
     */
    sendTask(serverId, task) {
        const socket = this.connections.get(serverId);
        if (!socket) {
            console.warn(`[AgentService] 无法下发任务: ${serverId} 不在线`);
            return false;
        }

        socket.emit(Events.DASHBOARD_TASK, {
            id: task.id || crypto.randomUUID(),
            type: task.type,
            data: task.data,
            timeout: task.timeout || 0
        });

        console.log(`[AgentService] 任务已下发: ${serverId} -> ${task.type}`);
        return true;
    }

    /**
     * 请求 Agent 上报主机信息
     */
    requestHostInfo(serverId) {
        return this.sendTask(serverId, {
            type: TaskTypes.REPORT_HOST_INFO,
            data: ''
        });
    }

    /**
     * 检查主机是否在线
     * @param {string} serverId
     * @returns {boolean}
     */
    isOnline(serverId) {
        return this.connections.has(serverId);
    }

    /**
     * 获取主机硬件信息
     * @param {string} serverId
     * @returns {Object|null}
     */
    getHostInfo(serverId) {
        return this.hostInfoCache.get(serverId) || null;
    }

    /**
     * 发送任务并等待结果
     * @param {string} serverId
     * @param {Object} task
     * @param {number} timeout - 超时时间 (毫秒)
     * @returns {Promise<Object>}
     */
    sendTaskAndWait(serverId, task, timeout = 60000) {
        return new Promise((resolve, reject) => {
            const taskId = task.id || crypto.randomUUID();
            const socket = this.connections.get(serverId);

            if (!socket) {
                return reject(new Error('主机不在线'));
            }

            // 设置超时
            const timer = setTimeout(() => {
                socket.off(Events.AGENT_TASK_RESULT, resultHandler);
                reject(new Error('任务执行超时'));
            }, timeout);

            // 结果处理器
            const resultHandler = (result) => {
                if (result.id === taskId) {
                    clearTimeout(timer);
                    socket.off(Events.AGENT_TASK_RESULT, resultHandler);
                    resolve(result);
                }
            };

            // 监听任务结果
            socket.on(Events.AGENT_TASK_RESULT, resultHandler);

            // 发送任务
            socket.emit(Events.DASHBOARD_TASK, {
                id: taskId,
                type: task.type,
                data: task.data,
                timeout: task.timeout || 0
            });

            console.log(`[AgentService] 同步任务已下发: ${serverId} -> ${task.type} (id: ${taskId})`);
        });
    }

    // ==================== 状态查询 ====================

    /**
     * 获取 Agent 指标 (兼容旧接口)
     */
    getMetrics(serverId) {
        // 优先返回 Socket.IO 缓存
        const cached = this.stateCache.get(serverId);
        if (cached) {
            const hostInfo = this.hostInfoCache.get(serverId) || {};
            return stateToFrontendFormat(cached.state, hostInfo);
        }

        // 降级到旧 HTTP 缓存
        return this.legacyMetrics.get(serverId);
    }

    /**
     * 获取 Agent 状态 (兼容旧接口)
     */
    getStatus(serverId) {
        // 优先检查 Socket.IO 连接
        if (this.connections.has(serverId)) {
            const cached = this.stateCache.get(serverId);
            return {
                connected: true,
                lastSeen: cached?.timestamp || Date.now(),
                version: this.hostInfoCache.get(serverId)?.agent_version || 'socket.io'
            };
        }

        // 降级到旧缓存
        const status = this.legacyStatus.get(serverId);
        if (!status) {
            return { connected: false, lastSeen: null };
        }

        const isOnline = Date.now() - status.lastSeen < 10000;
        return {
            ...status,
            connected: isOnline
        };
    }

    /**
     * 获取所有在线 Agent 列表
     */
    getOnlineAgents() {
        return Array.from(this.connections.keys());
    }

    /**
     * 获取连接统计
     */
    getConnectionStats() {
        return {
            online: this.connections.size,
            cached: this.stateCache.size,
            frontendClients: this.io?.of('/metrics').sockets.size || 0
        };
    }

    // ==================== 数据库状态同步 ====================

    /**
     * 更新数据库中的主机状态
     */
    updateServerStatus(serverId, status) {
        try {
            serverStorage.updateStatus(serverId, {
                status: status,
                last_check_time: new Date().toISOString(),
                last_check_status: status === 'online' ? 'success' : 'offline'
            });
        } catch (e) {
            // 主机可能不存在于数据库
        }
    }

    // ==================== 兼容旧 HTTP 推送 (过渡期) ====================

    /**
     * 处理 HTTP POST 推送的指标数据 (兼容旧 Agent)
     * @deprecated 将在未来版本移除
     */
    processMetrics(serverId, metrics) {
        const timestamp = Date.now();

        // 解析 CPU
        const cpuUsage = parseFloat(metrics.cpu) || 0;

        // 解析内存
        let memUsed = 0, memTotal = 0;
        if (metrics.mem) {
            const memMatch = metrics.mem.match(/(\d+)\/(\d+)/);
            if (memMatch) {
                memUsed = parseInt(memMatch[1]);
                memTotal = parseInt(memMatch[2]);
            }
        }

        // 解析磁盘
        let diskUsed = '', diskTotal = '', diskUsage = '';
        if (metrics.disk) {
            const diskMatch = metrics.disk.match(/([^/]+)\/([^\s]+)\s*\(?([.\d]+%?)?\)?/);
            if (diskMatch) {
                diskUsed = diskMatch[1];
                diskTotal = diskMatch[2];
                diskUsage = diskMatch[3] || '';
            }
        }

        const processedMetrics = {
            timestamp,
            cpu: cpuUsage,
            cpu_usage: `${cpuUsage}%`,
            mem: `${memUsed}/${memTotal}MB`,
            mem_usage: `${memUsed}/${memTotal}MB`,
            disk: metrics.disk,
            disk_used: diskUsed,
            disk_total: diskTotal,
            disk_usage: metrics.disk,
            load: metrics.load || '0 0 0',
            cores: parseInt(metrics.cores) || 1,
            network: {
                rx_speed: metrics.rx_speed || '0 B/s',
                tx_speed: metrics.tx_speed || '0 B/s',
                rx_total: metrics.rx_total || '0 B',
                tx_total: metrics.tx_total || '0 B',
                connections: parseInt(metrics.connections) || 0
            },
            docker: {
                installed: metrics.docker_installed === true || metrics.docker_installed === 'true',
                running: parseInt(metrics.docker_running) || 0,
                stopped: parseInt(metrics.docker_stopped) || 0,
                containers: Array.isArray(metrics.containers) ? metrics.containers : []
            }
        };

        // 存储到兼容缓存
        this.legacyMetrics.set(serverId, processedMetrics);
        this.legacyStatus.set(serverId, {
            lastSeen: timestamp,
            connected: true,
            version: metrics.agent_version || 'http-legacy'
        });

        // 广播给前端
        this.broadcastMetrics(serverId, processedMetrics);

        console.log(`[AgentService] HTTP 推送: ${serverId} -> CPU: ${processedMetrics.cpu_usage}`);

        return processedMetrics;
    }

    // ==================== 安装脚本生成 ====================

    /**
     * 生成新版 Agent 安装脚本 (Go Agent) - 支持无缝升级
     */
    generateInstallScript(serverId, serverUrl) {
        const agentKey = this.getAgentKey(serverId);
        const $ = '$'; // 用于在模板字符串中输出 $

        return `#!/bin/bash
# API Monitor Agent 自动安装/升级脚本 (Go 版)
# 支持从旧版 Node.js Agent 无缝升级

# 颜色定义
RED='\\033[0;31m'
GREEN='\\033[0;32m'
YELLOW='\\033[1;33m'
CYAN='\\033[0;36m'
NC='\\033[0m'

# 配置信息
SERVER_URL="${serverUrl}"
SERVER_ID="${serverId}"
AGENT_KEY="${agentKey}"
INSTALL_DIR="/opt/api-monitor-agent"
SERVICE_NAME="api-monitor-agent"

# 检测系统架构
ARCH=${$}(uname -m)
case ${$}ARCH in
    x86_64)
        BINARY_NAME="agent-linux-amd64"
        ;;
    aarch64|arm64)
        BINARY_NAME="agent-linux-arm64"
        ;;
    *)
        echo -e "${$}{RED}错误: 不支持的架构 ${$}ARCH${$}{NC}"
        exit 1
        ;;
esac
BINARY_URL="${$}{SERVER_URL}/agent/${$}{BINARY_NAME}"

# 1. 检查权限
if [ "${$}EUID" -ne 0 ]; then 
  echo -e "${$}{RED}错误: 请使用 sudo 运行此脚本${$}{NC}"
  exit 1
fi

# 2. 检测是否为升级安装
UPGRADE_MODE=false
if [ -d "${$}INSTALL_DIR" ]; then
    if [ -f "${$}INSTALL_DIR/agent-bin" ] || [ -f "${$}INSTALL_DIR/agent" ]; then
        UPGRADE_MODE=true
        echo -e "${$}{CYAN}>>> 检测到已安装 Agent，将执行升级...${$}{NC}"
    fi
fi

# 3. 停止现有服务
if systemctl is-active --quiet ${$}SERVICE_NAME 2>/dev/null; then
    echo -e "${$}{YELLOW}⏹ 停止现有服务...${$}{NC}"
    systemctl stop ${$}SERVICE_NAME
fi

# 4. 清理旧版文件 (Node.js Agent 残留)
if [ "${$}UPGRADE_MODE" = true ]; then
    echo -e "${$}{YELLOW}🧹 清理旧版 Agent 文件...${$}{NC}"
    rm -f "${$}INSTALL_DIR/agent-bin" 2>/dev/null
    rm -rf "${$}INSTALL_DIR/node_modules" 2>/dev/null
    rm -f "${$}INSTALL_DIR/package.json" "${$}INSTALL_DIR/package-lock.json" 2>/dev/null
    rm -f "${$}INSTALL_DIR/index.js" "${$}INSTALL_DIR/config.js" "${$}INSTALL_DIR/collector.js" 2>/dev/null
fi

# 5. 创建/进入目录
echo "📁 目录: ${$}INSTALL_DIR"
mkdir -p "${$}INSTALL_DIR"
cd "${$}INSTALL_DIR"

# 6. 下载新版二进制文件
echo -e "${$}{YELLOW}📥 下载 Agent 二进制文件 (${$}BINARY_NAME)...${$}{NC}"
curl -L -f -s "${$}BINARY_URL" -o agent.new
if [ ${$}? -ne 0 ]; then
    echo -e "${$}{RED}❌ 错误: 无法从 ${$}BINARY_URL 下载二进制文件。${$}{NC}"
    echo -e "${$}{YELLOW}请确保主控端已完成构建。${$}{NC}"
    if [ "${$}UPGRADE_MODE" = true ] && [ -f "${$}INSTALL_DIR/agent" ]; then
        echo -e "${$}{YELLOW}尝试恢复旧版服务...${$}{NC}"
        systemctl start ${$}SERVICE_NAME
    fi
    exit 1
fi

# 替换二进制文件 (原子操作)
mv agent.new agent
chmod +x agent

# 7. 生成/更新配置文件
echo -e "${$}{YELLOW}📝 生成配置文件...${$}{NC}"
if [ -f "config.json" ] && [ "${$}UPGRADE_MODE" = true ]; then
    echo -e "${$}{CYAN}   保留现有配置文件${$}{NC}"
else
    cat > config.json << 'CONFIGEOF'
{
    "serverUrl": "__SERVER_URL__",
    "serverId": "__SERVER_ID__",
    "agentKey": "__AGENT_KEY__",
    "reportInterval": 1500,
    "reconnectDelay": 4000
}
CONFIGEOF
    sed -i "s|__SERVER_URL__|${$}SERVER_URL|g" config.json
    sed -i "s|__SERVER_ID__|${$}SERVER_ID|g" config.json
    sed -i "s|__AGENT_KEY__|${$}AGENT_KEY|g" config.json
fi

# 8. 创建/更新 systemd 服务
echo -e "${$}{YELLOW}⚙️ 配置 systemd 服务...${$}{NC}"
cat > /etc/systemd/system/${$}SERVICE_NAME.service << SERVICEEOF
[Unit]
Description=API Monitor Agent (Go)
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=${$}INSTALL_DIR
ExecStart=${$}INSTALL_DIR/agent
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
SERVICEEOF

# 9. 启动服务
echo -e "${$}{YELLOW}🚀 启动服务...${$}{NC}"
systemctl daemon-reload
systemctl enable ${$}SERVICE_NAME
systemctl restart ${$}SERVICE_NAME

# 10. 检查状态
sleep 1
if systemctl is-active --quiet ${$}SERVICE_NAME; then
    echo -e "${$}{GREEN}================================================${$}{NC}"
    if [ "${$}UPGRADE_MODE" = true ]; then
        echo -e "${$}{GREEN}  ✅ API Monitor Agent 升级成功!${$}{NC}"
    else
        echo -e "${$}{GREEN}  ✅ API Monitor Agent 安装成功!${$}{NC}"
    fi
    echo -e "${$}{GREEN}  架构: ${$}ARCH (${$}BINARY_NAME)${$}{NC}"  
    echo -e "${$}{GREEN}  查看状态: systemctl status ${$}SERVICE_NAME${$}{NC}"
    echo -e "${$}{GREEN}  查看日志: journalctl -u ${$}SERVICE_NAME -f${$}{NC}"
    echo -e "${$}{GREEN}================================================${$}{NC}"
else
    echo -e "${$}{RED}❌ 服务启动失败，请检查日志:${$}{NC}"
    echo -e "${$}{RED}   journalctl -u ${$}SERVICE_NAME -n 20${$}{NC}"
    exit 1
fi
`;
    }

    /**
     * 生成 Windows (PowerShell) 安装脚本 - 支持无缝升级
     */
    generateWinInstallScript(serverId, serverUrl) {
        const agentKey = this.getAgentKey(serverId);

        return `
# API Monitor Agent Windows 自动安装/升级脚本 (Go 版)
# 支持从旧版 Node.js Agent 无缝升级
$ErrorActionPreference = "Stop"

$SERVER_URL = "${serverUrl}"
$SERVER_ID = "${serverId}"
$AGENT_KEY = "${agentKey}"
$INSTALL_DIR = "$env:LOCALAPPDATA\\api-monitor-agent"
$BINARY_URL = "$SERVER_URL/agent/agent-windows-amd64.exe"
$taskName = "APIMonitorAgent"

Write-Host ">>> API Monitor Agent 安装/升级脚本 (Go 版)" -ForegroundColor Cyan

# 1. 检测是否为升级安装
$upgradeMode = $false
$oldExe = Join-Path $INSTALL_DIR "api-monitor-agent.exe"
$newExe = Join-Path $INSTALL_DIR "agent.exe"

if ((Test-Path $oldExe) -or (Test-Path $newExe)) {
    $upgradeMode = $true
    Write-Host ">>> 检测到已安装 Agent，将执行升级..." -ForegroundColor Cyan
}

# 2. 停止现有任务
$existingTask = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
if ($existingTask) {
    Write-Host "⏹ 停止现有任务..." -ForegroundColor Yellow
    Stop-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
    Start-Sleep -Seconds 2
}

# 3. 清理旧版文件
if ($upgradeMode) {
    Write-Host "🧹 清理旧版 Agent 文件..." -ForegroundColor Yellow
    # 删除旧的 Node.js Agent 二进制
    if (Test-Path $oldExe) { Remove-Item $oldExe -Force }
    # 删除可能存在的 Node.js 文件
    $oldFiles = @("index.js", "config.js", "collector.js", "package.json", "package-lock.json")
    foreach ($f in $oldFiles) {
        $fp = Join-Path $INSTALL_DIR $f
        if (Test-Path $fp) { Remove-Item $fp -Force }
    }
    # 删除 node_modules
    $nodeModules = Join-Path $INSTALL_DIR "node_modules"
    if (Test-Path $nodeModules) { Remove-Item $nodeModules -Recurse -Force }
}

# 4. 创建目录
if (-not (Test-Path $INSTALL_DIR)) {
    Write-Host "📁 创建目录: $INSTALL_DIR"
    New-Item -ItemType Directory -Force -Path $INSTALL_DIR | Out-Null
}
Set-Location $INSTALL_DIR

# 5. 下载新版二进制文件
Write-Host "📥 下载 Agent 二进制文件..." -ForegroundColor Yellow
$tempExe = Join-Path $INSTALL_DIR "agent.exe.new"
try {
    Invoke-WebRequest -Uri $BINARY_URL -OutFile $tempExe
    # 原子替换
    if (Test-Path $newExe) { Remove-Item $newExe -Force }
    Rename-Item $tempExe "agent.exe"
} catch {
    Write-Host "❌ 下载失败: $_" -ForegroundColor Red
    if (Test-Path $tempExe) { Remove-Item $tempExe -Force }
    exit 1
}

# 6. 生成/更新配置文件
$configPath = Join-Path $INSTALL_DIR "config.json"
if ($upgradeMode -and (Test-Path $configPath)) {
    Write-Host "📝 保留现有配置文件" -ForegroundColor Cyan
} else {
    Write-Host "📝 生成配置文件..." -ForegroundColor Yellow
    $config = @{
        serverUrl = $SERVER_URL
        serverId = $SERVER_ID
        agentKey = $AGENT_KEY
        reportInterval = 1500
        reconnectDelay = 4000
    } | ConvertTo-Json
    $config | Out-File -FilePath $configPath -Encoding ASCII -Force
}

# 7. 设置并启动服务 (开机自启)
Write-Host "⚙️ 配置开机自启..." -ForegroundColor Yellow
$executablePath = Join-Path $INSTALL_DIR "agent.exe"

# 停止并删除已存在的同名任务
Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue | Unregister-ScheduledTask -Confirm:$false

$action = New-ScheduledTaskAction -Execute $executablePath -WorkingDirectory $INSTALL_DIR
$trigger = New-ScheduledTaskTrigger -AtLogOn
$settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries
Register-ScheduledTask -Action $action -Trigger $trigger -Settings $settings -TaskName $taskName -Description "API Monitor Agent Auto-start Task" | Out-Null

# 立即开始运行
Start-ScheduledTask -TaskName $taskName

Write-Host "================================================" -ForegroundColor Green
Write-Host "  ✅ API Monitor Agent 安装完成!" -ForegroundColor Green
Write-Host "  安装目录: $INSTALL_DIR" -ForegroundColor White
Write-Host "  自启配置: 已添加 Windows 计划任务 ($taskName)" -ForegroundColor White
Write-Host "  启动状态: 已在后台启动" -ForegroundColor White
Write-Host "================================================" -ForegroundColor Green
        `.trim();
    }

    /**
     * 生成卸载脚本
     */
    generateUninstallScript() {
        return `#!/bin/bash
# API Monitor Agent 卸载脚本

if [ "$EUID" -ne 0 ]; then 
  echo "请以 root 身份运行"
  exit 1
fi

SERVICE_NAME="api-monitor-agent"
INSTALL_DIR="/opt/api-monitor-agent"

echo "正在停止并移除 API Monitor Agent..."

systemctl stop \$SERVICE_NAME 2>/dev/null || true
systemctl disable \$SERVICE_NAME 2>/dev/null || true
rm -f /etc/systemd/system/\$SERVICE_NAME.service
systemctl daemon-reload

rm -rf "\$INSTALL_DIR"

echo "✅ 卸载完成"
`;
    }
}

module.exports = new AgentService();
