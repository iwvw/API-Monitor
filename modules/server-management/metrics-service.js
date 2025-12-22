/**
 * 实时指标推送服务 (SSH 驻留流模式)
 * 特点：长连接、零轮询、微开销。通过在服务端运行一个不退出的死循环脚本，
 * 持续监听 stdout 推送 JSON 指标，摆脱反复建立 SSH channel 的巨大消耗。
 */

const { WebSocketServer } = require('ws');
const net = require('net');
const sshService = require('./ssh-service');
const { createLogger } = require('../../src/utils/logger');
const models = require('../../src/db/models');

const logger = createLogger('MetricsService');

class MetricsService {
    constructor() {
        this.wss = null;
        this.clients = new Set();
        // 活跃流容器 serverId -> { stream, serverConfig, lastUpdate }
        this.activeStreams = new Map();
        // 重试状态追踪
        this.retryCounts = new Map();
        // 正在连接中的主机锁
        this.isConnecting = new Set();
        // 采集指令 (已移至 getStreamCommand 方法)

        // 历史数据采集相关
        this.historyCollectInterval = 5 * 60 * 1000; // 默认 5 分钟采集一次
        this.historyCollectTimer = null;
        this.latestMetrics = new Map(); // 缓存每台主机的最新指标 serverId -> metrics

        // TCP延迟缓存 serverId -> { latency, timestamp }
        this.latencyCache = new Map();
        // 延迟测量间隔 (30秒测一次)
        this.latencyMeasureInterval = 30 * 1000;
    }


    /**
     * 初始化 WebSocket 服务
     */
    init(server) {
        this.wss = new WebSocketServer({ noServer: true });
        logger.info('✓ 实时指标流服务已就绪 (驻留流模式)');

        this.wss.on('connection', (ws) => {
            this.clients.add(ws);
            logger.info(`前端 [STREAM] 已连接 (当前在线: ${this.clients.size})`);

            // 如果是第一个观察者，拉起所有主机的流
            if (this.clients.size === 1) {
                this.activateAllStreams();
            }

            ws.on('close', () => {
                this.clients.delete(ws);
                // 如果没有观察者了，关掉所有流以节省资源
                if (this.clients.size === 0) {
                    this.deactivateAllStreams();
                }
            });
        });

        return this.wss;
    }

    /**
     * 激活所有配置了监控的主机流
     */
    async activateAllStreams() {
        const servers = models.ServerAccount.getAll();
        // 每次重新激活时，给予一次重内置计数器的机会 (方便用户切页重试)
        this.retryCounts.clear();
        logger.info(`正在激活 ${servers.length} 台主机的驻留流通道...`);

        for (const server of servers) {
            this.startSingleStream(server);
        }
    }

    /**
     * 启动单个主机的驻留流 (带自适应重连保护)
     */
    async startSingleStream(server) {
        if (this.activeStreams.has(server.id)) return;
        if (this.isConnecting.has(server.id)) return;

        this.isConnecting.add(server.id);
        const info = this.retryCounts.get(server.id) || { count: 0, delay: 2500, batchCount: 0 };

        try {
            const isLongTerm = info.count > 10;
            const logMsg = isLongTerm
                ? `[${server.host}] 长期探测批次中 (${info.batchCount || 1}/5)...`
                : `[${server.host}] 尝试建立驻留流 (第 ${info.count + 1} 次尝试)...`;

            logger.debug(logMsg);
            const stream = await sshService.executeStream(server.id, server, this.getStreamCommand());

            this.activeStreams.set(server.id, {
                stream,
                serverConfig: server,
                lastUpdate: Date.now()
            });

            // 成功建立连接
            let buffer = '';
            stream.on('data', (data) => {
                // 收到真实数据，刷新连接池状态防止被回收
                const conn = sshService.connections.get(server.id);
                if (conn) conn.lastUsed = Date.now();

                // 彻底重置重试计数器
                if (this.retryCounts.has(server.id)) {
                    this.retryCounts.delete(server.id);
                }
                // ... (解析逻辑保持不变)
                buffer += data.toString();
                const lines = buffer.split('\n');
                buffer = lines.pop();
                for (const line of lines) {
                    if (line.includes('STREAM_JSON:')) {
                        try {
                            const jsonStr = line.split('STREAM_JSON:')[1].trim();
                            const metrics = JSON.parse(jsonStr);
                            this.broadcastToClients(server.id, metrics);
                            // 缓存最新指标用于历史采集
                            this.latestMetrics.set(server.id, {
                                ...metrics,
                                timestamp: Date.now()
                            });
                        } catch (e) { }
                    }
                }
            });

            stream.on('close', () => this.handleStreamFailure(server));

        } catch (error) {
            logger.error(`[${server.host}] 建立流失败: ${error.message}`);
            this.handleStreamFailure(server);
        } finally {
            this.isConnecting.delete(server.id);
        }
    }

    /**
     * 统一处理流失败，实现 10次后每10分钟重试5次 的逻辑
     */
    handleStreamFailure(server) {
        this.activeStreams.delete(server.id);
        if (this.clients.size === 0) return;

        let info = this.retryCounts.get(server.id) || { count: 0, delay: 2500, batchCount: 0 };
        info.count++;

        let nextDelay;

        if (info.count <= 10) {
            // 阶段 1: 初始指数退避 (快速寻回连接)
            nextDelay = Math.min(info.delay * 2, 300000); // 最高 5 分钟
            info.delay = nextDelay;
            logger.warn(`[${server.host}] 自动退避中，将在 ${nextDelay / 1000} 秒后重试`);
        } else {
            // 阶段 2: 长期故障维护 (每10分钟重试5次)
            if (info.batchCount < 5) {
                nextDelay = 5000; // 批次内短间隔探测
                info.batchCount++;
                logger.warn(`[${server.host}] 长期熔断保护：批次内重试 (${info.batchCount}/5)，5秒后尝试`);
            } else {
                nextDelay = 600000; // 批次结束，冷却 10 分钟
                info.batchCount = 1; // 重置下一批次的计数
                logger.warn(`[${server.host}] 批次尝试均失败，进入 10 分钟深度冷却期...`);
            }
        }

        this.retryCounts.set(server.id, info);
        setTimeout(() => this.startSingleStream(server), nextDelay);
    }

    /**
     * 获取完整的探测脚本
     */
    getStreamCommand() {
        return `
            # 开启循环采集
            while true; do
                # 检查父进程 (sshd) 是否存在，不存在则自杀，防止孤儿进程
                if [ ! -d "/proc/$PPID" ]; then exit 0; fi
                
                # 采集负载
                L=$(cat /proc/loadavg | awk '{print $1,$2,$3}')
                # 采集核心数
                N=$(nproc 2>/dev/null || grep -c ^processor /proc/cpuinfo)
                # 采集内存 (取已用和总量)
                M=$(free -m | awk 'NR==2{printf "%d/%dMB", $3, $2}')
                # 采集CPU
                C=$(grep 'cpu ' /proc/stat | awk '{u=($2+$4)*100/($2+$4+$5)} END {printf "%.1f", u}')
                # 采集磁盘
                D=$(df -h / | awk 'NR==2{printf "%s/%s (%s)", $3, $2, $5}')
                
                # 采集 Docker (统计运行和停止数量)
                if command -v docker >/dev/null 2>&1; then
                    DR=$(docker ps -q | wc -l | tr -d ' ')
                    DT=$(docker ps -a -q | wc -l | tr -d ' ')
                    DS=$((DT - DR))
                    DI=true
                else
                    DR=0
                    DS=0
                    DI=false
                fi
                
                # 输出包裹 JSON，加前缀防止流粘包
                echo "STREAM_JSON:{\\\"load\\\":\\\"$L\\\",\\\"cores\\\":\\\"$N\\\",\\\"mem\\\":\\\"$M\\\",\\\"cpu\\\":\\\"$C\\\",\\\"disk\\\":\\\"$D\\\",\\\"docker_installed\\\":$DI,\\\"docker_running\\\":$DR,\\\"docker_stopped\\\":$DS}"
                sleep 1
            done
        `;
    }

    /**
     * 关闭所有流
     */
    deactivateAllStreams() {
        logger.info('无在线观察者，正在释放所有 SSH 驻留流...');
        for (const [serverId, info] of this.activeStreams) {
            try {
                // 发送结束信号或断开管道
                if (info.stream.writable) {
                    info.stream.end('\x03'); // 发送 Ctrl+C
                }
                info.stream.destroy();
            } catch (err) { }
        }
        this.activeStreams.clear();
    }

    /**
     * 将解析出的指标推送给前端
     */
    broadcastToClients(serverId, metrics) {
        if (this.clients.size === 0) return;

        const payload = JSON.stringify({
            type: 'metrics_update',
            data: [{
                serverId,
                metrics: {
                    load: metrics.load,
                    cores: metrics.cores,
                    mem_usage: metrics.mem,
                    cpu_usage: metrics.cpu + '%',
                    disk_usage: metrics.disk,
                    docker: {
                        installed: metrics.docker_installed,
                        running: metrics.docker_running,
                        stopped: metrics.docker_stopped
                    },
                    lastUpdate: new Date().toLocaleTimeString()
                }
            }]
        });

        this.clients.forEach(ws => {
            if (ws.readyState === 1) ws.send(payload);
        });

        // 异步测量并更新延迟（不阻塞推送）
        this.measureLatencyIfNeeded(serverId);
    }

    /**
     * 按需测量延迟（有缓存时跳过）
     */
    async measureLatencyIfNeeded(serverId) {
        const cached = this.latencyCache.get(serverId);
        const now = Date.now();

        // 如果缓存存在且未过期，跳过
        if (cached && (now - cached.timestamp) < this.latencyMeasureInterval) {
            return cached.latency;
        }

        // 获取服务器配置
        const streamInfo = this.activeStreams.get(serverId);
        if (!streamInfo || !streamInfo.serverConfig) return null;

        const server = streamInfo.serverConfig;

        try {
            const latency = await this.tcpPing(server.host, server.port || 22);

            // 更新缓存
            this.latencyCache.set(serverId, { latency, timestamp: now });

            // 更新数据库中的 response_time
            const { ServerAccount } = models;
            ServerAccount.updateStatus(serverId, { response_time: latency });

            return latency;
        } catch (error) {
            // ping 失败，不更新缓存
            return null;
        }
    }

    /**
     * TCP Ping - 测量 TCP 端口连接延迟
     * 使用 performance 计时获取更精确的延迟
     */
    tcpPing(host, port, timeout = 3000) {
        return new Promise((resolve, reject) => {
            const { performance } = require('perf_hooks');
            const startTime = performance.now();
            const socket = new net.Socket();

            // 禁用 Nagle 算法，确保立即发送
            socket.setNoDelay(true);
            socket.setTimeout(timeout);

            socket.on('connect', () => {
                const latency = Math.round(performance.now() - startTime);
                socket.destroy();
                resolve(latency);
            });

            socket.on('timeout', () => {
                socket.destroy();
                reject(new Error('TCP ping timeout'));
            });

            socket.on('error', (err) => {
                socket.destroy();
                reject(err);
            });

            socket.connect(port, host);
        });
    }

    /**
     * 兼容升级处理
     */
    handleUpgrade(request, socket, head, callback) {
        this.wss.handleUpgrade(request, socket, head, callback);
    }

    /**
     * 启动历史数据采集定时器
     * @param {number} intervalMs - 采集间隔（毫秒），默认 5 分钟
     */
    startHistoryCollector(intervalMs = null) {
        if (this.historyCollectTimer) {
            logger.warn('历史采集器已在运行中');
            return;
        }

        const interval = intervalMs || this.historyCollectInterval;
        logger.info(`📊 启动历史指标采集器 (间隔: ${interval / 1000}s)`);

        // 立即执行一次采集
        this.collectHistorySnapshot();

        // 启动定时采集
        this.historyCollectTimer = setInterval(() => {
            this.collectHistorySnapshot();
        }, interval);
    }

    /**
     * 停止历史数据采集定时器
     */
    stopHistoryCollector() {
        if (this.historyCollectTimer) {
            clearInterval(this.historyCollectTimer);
            this.historyCollectTimer = null;
            logger.info('📊 历史指标采集器已停止');
        }
    }

    /**
     * 执行一次历史快照采集
     * 将缓存的最新指标批量写入数据库
     */
    collectHistorySnapshot() {
        if (this.latestMetrics.size === 0) {
            logger.debug('无可采集的指标数据');
            return;
        }

        const { ServerMetricsHistory } = require('./models');
        const records = [];
        const now = Date.now();
        const maxAge = 2 * 60 * 1000; // 2 分钟内的数据才有效

        for (const [serverId, metrics] of this.latestMetrics) {
            // 跳过过期数据
            if (now - metrics.timestamp > maxAge) {
                continue;
            }

            // 解析内存数据 (格式: "123/1024MB")
            let memUsed = 0, memTotal = 0, memUsage = 0;
            const memMatch = metrics.mem?.match(/(\d+)\/(\d+)MB/);
            if (memMatch) {
                memUsed = parseInt(memMatch[1]);
                memTotal = parseInt(memMatch[2]);
                memUsage = memTotal > 0 ? Math.round((memUsed / memTotal) * 100) : 0;
            }

            // 解析磁盘数据 (格式: "10G/50G (20%)")
            let diskUsed = '', diskTotal = '', diskUsage = 0;
            const diskMatch = metrics.disk?.match(/([^\/]+)\/([^\s]+)\s\(([\d%.]+)\)/);
            if (diskMatch) {
                diskUsed = diskMatch[1];
                diskTotal = diskMatch[2];
                diskUsage = parseFloat(diskMatch[3]) || 0;
            }

            records.push({
                server_id: serverId,
                cpu_usage: parseFloat(metrics.cpu) || 0,
                cpu_load: metrics.load || '',
                cpu_cores: parseInt(metrics.cores) || 0,
                mem_used: memUsed,
                mem_total: memTotal,
                mem_usage: memUsage,
                disk_used: diskUsed,
                disk_total: diskTotal,
                disk_usage: diskUsage,
                docker_installed: metrics.docker_installed || false,
                docker_running: metrics.docker_running || 0,
                docker_stopped: metrics.docker_stopped || 0
            });
        }

        if (records.length > 0) {
            try {
                const count = ServerMetricsHistory.createMany(records);
                logger.info(`📊 已采集 ${count} 条历史指标记录`);
            } catch (error) {
                logger.error('历史指标采集失败:', error.message);
            }
        }
    }

    /**
     * 获取采集器状态
     */
    getCollectorStatus() {
        return {
            isRunning: !!this.historyCollectTimer,
            interval: this.historyCollectInterval,
            cachedServers: this.latestMetrics.size,
            activeStreams: this.activeStreams.size,
            connectedClients: this.clients.size
        };
    }

    /**
     * 设置采集间隔
     * @param {number} intervalMs - 新的采集间隔（毫秒）
     */
    setCollectInterval(intervalMs) {
        this.historyCollectInterval = intervalMs;
        // 如果采集器正在运行，重启以应用新间隔
        if (this.historyCollectTimer) {
            this.stopHistoryCollector();
            this.startHistoryCollector(intervalMs);
        }
    }
}

module.exports = new MetricsService();

