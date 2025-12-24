/**
 * 监控指标模块
 * 负责实时指标流、轮询、历史记录、图表渲染等
 */

/**
 * 监控指标方法集合
 */
export const metricsMethods = {
    // ==================== 日志与轮询 ====================

    async loadMonitorLogs(page) {
        if (typeof page === 'number') {
            this.logPage = page;
        }

        this.monitorLogsLoading = true;

        try {
            const params = new URLSearchParams({
                page: this.logPage,
                pageSize: this.logPageSize
            });

            if (this.logFilter.serverId) {
                params.append('serverId', this.logFilter.serverId);
            }
            if (this.logFilter.status) {
                params.append('status', this.logFilter.status);
            }

            const response = await fetch(`/api/server/monitor/logs?${params}`, {
                headers: this.getAuthHeaders()
            });
            const data = await response.json();

            if (data.success) {
                this.monitorLogs = data.data;
            } else {
                this.showGlobalToast('加载日志失败: ' + data.error, 'error');
            }
        } catch (error) {
            console.error('加载监控日志失败:', error);
            this.showGlobalToast('加载监控日志失败', 'error');
        } finally {
            this.monitorLogsLoading = false;
        }
    },

    startServerPolling() {
        // 关键决策：若有 WebSocket 实时流，则无需发起任何 HTTP 主动探测
        if (this.metricsWsConnected) {
            if (this.serverPollingTimer) {
                console.warn('🛡️ 实时流已接管，正在休眠后台轮询任务');
                this.stopServerPolling();
            }
            return;
        }

        // 确保只有一个轮询定时器在运行
        if (this.serverPollingTimer) return;

        const interval = Math.max(30000, (this.monitorConfig.interval || 60) * 1000);
        console.log(`📡 实时流不可用，启动后台降级轮询 (${interval / 1000}s)`);

        // 重置倒计时
        this.serverRefreshCountdown = Math.floor(interval / 1000);
        this.serverRefreshProgress = 100;

        // 启动倒计时定时器 (仅在可见时运行)
        this.serverCountdownInterval = setInterval(() => {
            if (document.visibilityState !== 'visible') return;

            if (this.serverRefreshCountdown > 0) {
                this.serverRefreshCountdown--;
                this.serverRefreshProgress = (this.serverRefreshCountdown / (interval / 1000)) * 100;
            }
        }, 1000);

        // 启动主轮询定时器
        this.serverPollingTimer = setInterval(() => {
            // 只要可见且已认证就探测，不再局限于 server 标签页
            if (document.visibilityState === 'visible' && this.isAuthenticated) {
                this.probeAllServers();
                // 重置倒计时
                this.serverRefreshCountdown = Math.floor(interval / 1000);
                this.serverRefreshProgress = 100;
            }
        }, interval);
    },

    stopServerPolling() {
        if (this.serverPollingTimer) {
            clearInterval(this.serverPollingTimer);
            this.serverPollingTimer = null;
        }
        if (this.serverCountdownInterval) {
            clearInterval(this.serverCountdownInterval);
            this.serverCountdownInterval = null;
        }
    },

    // ==================== Socket.IO 实时流 ====================

    /**
     * 动态加载 Socket.IO 客户端
     */
    async loadSocketIO() {
        if (window.io) return true;

        const CDN_SOURCES = [
            'https://registry.npmmirror.com/socket.io-client/4.7.2/files/dist/socket.io.min.js',
            'https://cdn.jsdelivr.net/npm/socket.io-client@4.7.2/dist/socket.io.min.js',
            'https://unpkg.com/socket.io-client@4.7.2/dist/socket.io.min.js'
        ];

        for (let i = 0; i < CDN_SOURCES.length; i++) {
            const src = CDN_SOURCES[i];
            console.log(`[Metrics] 加载 Socket.IO 客户端 (${i + 1}/${CDN_SOURCES.length})...`);

            try {
                await new Promise((resolve, reject) => {
                    const script = document.createElement('script');
                    script.src = src;
                    script.async = true;
                    script.onload = () => {
                        if (window.io) {
                            console.log(`[Metrics] ✅ Socket.IO 客户端加载成功`);
                            resolve();
                        } else {
                            reject(new Error('io not available'));
                        }
                    };
                    script.onerror = () => reject(new Error('Failed to load'));
                    setTimeout(() => reject(new Error('Timeout')), 5000);
                    document.head.appendChild(script);
                });
                return true;
            } catch (err) {
                console.warn(`[Metrics] ❌ CDN 源不可用: ${src.split('/')[2]}`);
            }
        }

        console.error('[Metrics] 所有 Socket.IO CDN 源均不可用');
        return false;
    },

    async connectMetricsStream() {
        if (!this.isAuthenticated) {
            console.warn('⚠️ 尝试连接实时流失败: 用户未登录');
            return;
        }

        if (this.metricsWsConnected || this.metricsWsConnecting) {
            console.warn('ℹ️ 实时指标流已在连接中或已连接');
            return;
        }

        this.metricsWsConnecting = true;

        // 动态加载 Socket.IO 客户端
        const loaded = await this.loadSocketIO();
        if (!loaded) {
            console.warn('[Metrics] Socket.IO 加载失败，降级到 HTTP 轮询');
            this.metricsWsConnecting = false;
            this.startServerPolling();
            return;
        }

        console.log('🚀 正在连接 Socket.IO 实时流...');

        try {
            // 连接到 /metrics 命名空间
            const socket = window.io('/metrics', {
                reconnection: true,
                reconnectionDelay: 1000,
                reconnectionDelayMax: 5000,
                reconnectionAttempts: Infinity,
                transports: ['websocket', 'polling']
            });

            socket.on('connect', () => {
                this.metricsWsConnected = true;
                this.metricsWsConnecting = false;
                console.log('✅ Socket.IO 实时流已连接');

                // 停止 HTTP 轮询
                this.stopServerPolling();
            });

            // 单个主机指标更新
            socket.on('metrics:update', (data) => {
                if (data && data.serverId && data.metrics) {
                    this.handleSingleMetricUpdate(data);
                }
            });

            // 批量指标更新 (初始连接时)
            socket.on('metrics:batch', (dataArray) => {
                if (Array.isArray(dataArray)) {
                    dataArray.forEach(data => this.handleSingleMetricUpdate(data));
                }
            });

            // 主机状态变更
            socket.on('server:status', (data) => {
                if (data && data.serverId) {
                    this.updateServerStatus(data.serverId, data.status);
                }
            });

            socket.on('disconnect', (reason) => {
                this.metricsWsConnected = false;
                this.metricsWsConnecting = false;
                console.warn('❌ Socket.IO 连接断开:', reason);

                // 如果不是主动断开，启动轮询作为降级
                if (reason === 'io server disconnect' || reason === 'transport close') {
                    console.log('[Metrics] 启动 HTTP 轮询作为降级...');
                    this.startServerPolling();
                }
            });

            socket.on('connect_error', (err) => {
                console.error('[Metrics] Socket.IO 连接错误:', err.message);
                this.metricsWsConnecting = false;
            });

            this.metricsSocket = socket;

        } catch (err) {
            console.error('[Metrics] Socket.IO 初始化失败:', err);
            this.metricsWsConnecting = false;
            this.startServerPolling();
        }
    },

    /**
     * 处理单个主机的指标更新 (Socket.IO 事件格式)
     * 优化: 使用增量更新避免不必要的 Vue 响应式触发
     */
    handleSingleMetricUpdate(data) {
        if (!data || !data.serverId || !data.metrics) return;

        const server = this.serverList.find(s => s.id === data.serverId);
        if (!server) return;

        try {
            const metrics = data.metrics;

            // 确保 info 对象存在，但不替换整个对象
            if (!server.info) {
                server.info = {
                    cpu: {}, memory: {}, disk: [], network: {}, docker: {}
                };
            }
            const info = server.info;

            // 增量更新 CPU (仅在值变化时更新)
            const newCpuLoad = metrics.load || '-';
            const newCpuUsage = metrics.cpu_usage || '0%';
            const newCpuCores = metrics.cores || '-';

            if (!info.cpu) info.cpu = {};
            if (info.cpu.Load !== newCpuLoad) info.cpu.Load = newCpuLoad;
            if (info.cpu.Usage !== newCpuUsage) info.cpu.Usage = newCpuUsage;
            if (info.cpu.Cores !== newCpuCores) info.cpu.Cores = newCpuCores;

            // 增量更新内存
            if (metrics.mem_usage || metrics.mem) {
                const memStr = metrics.mem_usage || metrics.mem || '';
                const memMatch = memStr.match(/(\d+)\/(\d+)MB/);
                if (memMatch) {
                    const used = parseInt(memMatch[1]);
                    const total = parseInt(memMatch[2]);
                    const usagePercent = Math.round((used / total) * 100) + '%';
                    const usedStr = used + ' MB';
                    const totalStr = total + ' MB';

                    if (!info.memory) info.memory = {};
                    if (info.memory.Used !== usedStr) info.memory.Used = usedStr;
                    if (info.memory.Total !== totalStr) info.memory.Total = totalStr;
                    if (info.memory.Usage !== usagePercent) info.memory.Usage = usagePercent;
                }
            }

            // 增量更新磁盘
            if (metrics.disk_usage || metrics.disk) {
                const diskStr = metrics.disk_usage || metrics.disk || '';
                // 匹配格式: "473.78 GB/1.49 TB (31%)"
                const diskMatch = diskStr.match(/(.+?)\/(.+?)\s*\((\d+%?)\)/);
                if (diskMatch) {
                    if (!Array.isArray(info.disk)) info.disk = [{}];
                    if (!info.disk[0]) info.disk[0] = {};

                    if (info.disk[0].device !== '/') info.disk[0].device = '/';
                    if (info.disk[0].used !== diskMatch[1].trim()) info.disk[0].used = diskMatch[1].trim();
                    if (info.disk[0].total !== diskMatch[2].trim()) info.disk[0].total = diskMatch[2].trim();
                    if (info.disk[0].usage !== diskMatch[3]) info.disk[0].usage = diskMatch[3];
                }
            }

            // 增量更新 Docker
            if (metrics.docker) {
                if (!info.docker) info.docker = {};

                const installed = !!metrics.docker.installed;
                const running = metrics.docker.running || 0;
                const stopped = metrics.docker.stopped || 0;

                if (info.docker.installed !== installed) info.docker.installed = installed;
                if (info.docker.runningCount !== running) info.docker.runningCount = running;
                if (info.docker.stoppedCount !== stopped) info.docker.stoppedCount = stopped;

                // 容器列表只在数量变化时更新
                const newContainers = Array.isArray(metrics.docker.containers) ? metrics.docker.containers : [];
                if (!info.docker.containers || info.docker.containers.length !== newContainers.length) {
                    info.docker.containers = newContainers;
                }
            }

            // 增量更新网络
            if (metrics.network) {
                if (!info.network) info.network = {};
                Object.keys(metrics.network).forEach(key => {
                    if (info.network[key] !== metrics.network[key]) {
                        info.network[key] = metrics.network[key];
                    }
                });
            }

            // 更新时间戳 (简化格式)
            const newTimestamp = new Date(data.timestamp || Date.now()).toLocaleTimeString();
            if (info.lastUpdate !== newTimestamp) info.lastUpdate = newTimestamp;

            // 仅在状态变化时更新
            if (server.status !== 'online') server.status = 'online';
            if (server.error !== null) server.error = null;

        } catch (err) {
            console.warn('[Metrics] 数据转换失败:', err, data);
        }
    },

    /**
     * 更新主机状态
     */
    updateServerStatus(serverId, status) {
        const server = this.serverList.find(s => s.id === serverId);
        if (server) {
            server.status = status;
            if (status === 'offline') {
                server.error = 'Agent 离线';
            }
        }
    },

    closeMetricsStream() {
        if (this.metricsSocket) {
            this.metricsSocket.disconnect();
            this.metricsSocket = null;
        }
        // 兼容旧的 WebSocket
        if (this.metricsWs) {
            this.metricsWs.close();
            this.metricsWs = null;
        }
        this.metricsWsConnected = false;
    },

    handleMetricsUpdate(data) {
        if (!data || !Array.isArray(data)) return;

        // 智能更新 serverList 中的数据
        data.forEach(item => {
            if (!item || !item.serverId || !item.metrics) return;

            const server = this.serverList.find(s => s.id === item.serverId);
            if (!server) return;

            // 1. 准备/初始化结构
            // 如果 server.info 不存在，先创建一个完整的基础镜像，避免多次触发 Fragment 更新
            const isNewInfo = !server.info;
            const info = server.info ? { ...server.info } : {
                cpu: { Load: '-', Usage: '0%', Cores: '-' },
                memory: { Used: '-', Total: '-', Usage: '0%' },
                disk: [{ device: '/', used: '-', total: '-', usage: '0%' }],
                network: { connections: 0, rx_speed: '0 B/s', tx_speed: '0 B/s', rx_total: '-', tx_total: '-' },
                system: {},
                docker: { installed: false, containers: [] }
            };

            try {
                // 2. 更新 CPU 数据
                info.cpu = {
                    Load: item.metrics.load || '-',
                    Usage: item.metrics.cpu_usage || '0%',
                    Cores: item.metrics.cores || '-'
                };

                // 3. 更新内存数据 (逻辑增强：解析 "123/1024MB")
                if (item.metrics.mem_usage && typeof item.metrics.mem_usage === 'string') {
                    const memMatch = item.metrics.mem_usage.match(/(\d+)\/(\d+)MB/);
                    if (memMatch) {
                        const used = parseInt(memMatch[1]);
                        const total = parseInt(memMatch[2]);
                        info.memory = {
                            Used: used + ' MB',
                            Total: total + ' MB',
                            Usage: Math.round((used / total) * 100) + '%'
                        };
                    }
                }

                // 4. 更新磁盘数据 (逻辑增强：解析 "10G/50G (20%)")
                if (item.metrics.disk_usage && typeof item.metrics.disk_usage === 'string') {
                    const diskMatch = item.metrics.disk_usage.match(/([^\/]+)\/([^\s]+)\s\(([\d%.]+)\)/);
                    if (diskMatch) {
                        // 确保 info.disk 是数组类型（可能从后端传来的是字符串）
                        if (!Array.isArray(info.disk)) {
                            info.disk = [{}];
                        }
                        info.disk[0] = {
                            device: '/',
                            used: diskMatch[1],
                            total: diskMatch[2],
                            usage: diskMatch[3]
                        };
                    }
                }

                // 5. 更新 Docker 概要信息 (确保 containers 数组始终存在)
                if (item.metrics.docker) {
                    info.docker = {
                        ...(info.docker || {}),
                        installed: !!item.metrics.docker.installed,
                        runningCount: item.metrics.docker.running || 0,
                        stoppedCount: item.metrics.docker.stopped || 0,
                        containers: Array.isArray(item.metrics.docker.containers) ? item.metrics.docker.containers : (info.docker?.containers || [])
                    };
                }
                // 兜底：确保 docker.containers 始终是数组
                if (!info.docker) {
                    info.docker = { installed: false, containers: [] };
                } else if (!Array.isArray(info.docker.containers)) {
                    info.docker.containers = [];
                }

                // 6. 更新网络信息
                if (item.metrics.network) {
                    info.network = {
                        ...(info.network || {}),
                        ...item.metrics.network
                    };
                }

                // 赋值回响应式对象
                // 如果是新对象，直接赋值；如果是旧对象，赋值新引用以触发更干净的 Patch
                server.info = info;
                server.status = 'online';
                server.error = null;

            } catch (err) {
                console.warn('[Metrics] 数据转换失败:', err, item);
            }
        });
    },

    // ==================== 主动探测 ====================

    async probeAllServers() {
        this.probeStatus = 'loading';
        try {
            const response = await fetch('/api/server/check-all', { method: 'POST' });
            const data = await response.json();
            if (data.success) {
                this.probeStatus = 'success';
                await this.loadServerList();
            } else {
                this.probeStatus = 'error';
            }
        } catch (error) {
            console.error('探测主机失败:', error);
            this.probeStatus = 'error';
        }
        setTimeout(() => { this.probeStatus = ''; }, 3000);
    },

    // ==================== 历史指标 ====================

    async loadMetricsHistory(page = null) {
        if (page !== null) {
            this.metricsHistoryPagination.page = page;
        }

        this.metricsHistoryLoading = true;

        try {
            // 计算时间范围 (使用 UTC 时间)
            let startTime = null;
            const now = Date.now();

            switch (this.metricsHistoryTimeRange) {
                case '1h':
                    startTime = new Date(now - 60 * 60 * 1000).toISOString();
                    break;
                case '6h':
                    startTime = new Date(now - 6 * 60 * 60 * 1000).toISOString();
                    break;
                case '24h':
                    startTime = new Date(now - 24 * 60 * 60 * 1000).toISOString();
                    break;
                case '7d':
                    startTime = new Date(now - 7 * 24 * 60 * 60 * 1000).toISOString();
                    break;
                case 'all':
                default:
                    startTime = null;
            }

            console.log('[History] 查询时间范围:', this.metricsHistoryTimeRange, '起始时间:', startTime);

            // 移除分页逻辑，一次性加载所有数据
            const params = new URLSearchParams({
                page: 1, // 始终第一页
                pageSize: 10000 // 足够大的页容量以获取该时间段内所有数据
            });

            if (this.metricsHistoryFilter.serverId) {
                params.append('serverId', this.metricsHistoryFilter.serverId);
            }

            if (startTime) {
                params.append('startTime', startTime);
            }

            const response = await fetch(`/api/server/metrics/history?${params}`, {
                headers: this.getAuthHeaders()
            });
            const data = await response.json();

            if (data.success) {
                this.metricsHistoryList = data.data;
                this.metricsHistoryTotal = data.pagination.total;
                this.metricsHistoryPagination = {
                    page: data.pagination.page,
                    pageSize: data.pagination.pageSize,
                    totalPages: data.pagination.totalPages
                };
            } else {
                this.showGlobalToast('加载历史记录失败: ' + data.error, 'error');
            }

            // 同时加载采集器状态
            this.loadCollectorStatus();

            // 渲染图表
            this.$nextTick(() => {
                this.renderMetricsCharts();
            });
        } catch (error) {
            console.error('加载历史指标失败:', error);
            this.showGlobalToast('加载历史指标失败', 'error');
        } finally {
            this.metricsHistoryLoading = false;
        }
    },

    setMetricsTimeRange(range) {
        this.metricsHistoryTimeRange = range;
        this.loadMetricsHistory(1);

        // 如果主机列表有展开的卡片，同步刷新它们的图表
        if (this.expandedServers && this.expandedServers.length > 0) {
            this.expandedServers.forEach(serverId => {
                this.loadCardMetrics(serverId);
            });
        }
    },

    async triggerMetricsCollect() {
        try {
            const response = await fetch('/api/server/metrics/collect', { method: 'POST' });
            const data = await response.json();

            if (data.success) {
                this.showGlobalToast('已触发历史指标采集', 'success');
                setTimeout(() => this.loadMetricsHistory(), 1000);
            } else {
                this.showGlobalToast('触发采集失败: ' + data.error, 'error');
            }
        } catch (error) {
            console.error('触发采集失败:', error);
            this.showGlobalToast('触发采集失败', 'error');
        }
    },

    async clearMetricsHistory() {
        const confirmMsg = this.metricsHistoryFilter.serverId
            ? '确定要清空该主机的历史指标记录吗？'
            : '确定要清空所有主机的历史指标记录吗？此操作不可撤销！';

        if (!confirm(confirmMsg)) return;

        try {
            const params = new URLSearchParams();
            if (this.metricsHistoryFilter.serverId) {
                params.append('serverId', this.metricsHistoryFilter.serverId);
            }

            const response = await fetch(`/api/server/metrics/history/clear?${params}`, {
                method: 'DELETE',
                headers: this.getAuthHeaders()
            });
            const data = await response.json();

            if (data.success) {
                this.showGlobalToast(data.message, 'success');
                this.metricsHistoryList = [];
                this.metricsHistoryTotal = 0;
                this.loadMetricsHistory(1);
            } else {
                this.showGlobalToast('清空失败: ' + data.error, 'error');
            }
        } catch (error) {
            console.error('清空历史指标失败:', error);
            this.showGlobalToast('清空历史指标失败', 'error');
        }
    },

    // ==================== 图表渲染 ====================

    /**
     * 动态加载 Chart.js 的 CDN 回退机制
     * 依次尝试多个 CDN 源，直到成功加载
     */
    async loadChartJsFallback() {
        // 如果已加载则跳过
        if (window.Chart) return true;

        const CDN_SOURCES = [
            'https://registry.npmmirror.com/chart.js/4.4.7/files/dist/chart.umd.js', // npmmirror
            'https://cdn.jsdelivr.net/npm/chart.js@4.4.7/dist/chart.umd.js',         // jsDelivr
            'https://unpkg.com/chart.js@4.4.7/dist/chart.umd.js',                    // unpkg
            'https://cdnjs.cloudflare.com/ajax/libs/Chart.js/4.4.7/chart.umd.js'     // cdnjs
        ];

        for (let i = 0; i < CDN_SOURCES.length; i++) {
            const src = CDN_SOURCES[i];
            console.log(`[Charts] 尝试加载 Chart.js (${i + 1}/${CDN_SOURCES.length}): ${src.split('/')[2]}`);

            try {
                await new Promise((resolve, reject) => {
                    const script = document.createElement('script');
                    script.src = src;
                    script.async = true;
                    script.onload = () => {
                        if (window.Chart) {
                            console.log(`[Charts] ✅ Chart.js 加载成功 (来源: ${src.split('/')[2]})`);
                            resolve();
                        } else {
                            reject(new Error('Script loaded but Chart not available'));
                        }
                    };
                    script.onerror = () => reject(new Error(`Failed to load: ${src}`));
                    // 超时保护 (5秒)
                    setTimeout(() => reject(new Error('Timeout')), 5000);
                    document.head.appendChild(script);
                });
                return true; // 成功加载
            } catch (err) {
                console.warn(`[Charts] ❌ CDN 源不可用: ${src.split('/')[2]} - ${err.message}`);
            }
        }

        console.error('[Charts] 所有 CDN 源均不可用，图表功能已禁用');
        return false;
    },

    async renderMetricsCharts(retryCount = 0) {
        // 手机端不渲染图表，以最大化内容显示并节省性能
        if (window.innerWidth <= 768) return;

        // CDN 模式下 Chart.js 可能还未加载，使用回退机制动态加载
        if (!window.Chart) {
            if (retryCount < 2) {
                console.log(`[Charts] Chart.js 未就绪，${(retryCount + 1) * 300}ms 后重试...`);
                setTimeout(() => this.renderMetricsCharts(retryCount + 1), 300);
                return;
            }

            // 重试用尽，启动多源回退加载
            console.log('[Charts] 正在启动 CDN 多源回退加载...');
            const loaded = await this.loadChartJsFallback();
            if (!loaded) {
                console.warn('[Charts] Chart.js 加载失败，跳过图表渲染');
                return;
            }
        }

        if (!this.groupedMetricsHistory) return;

        Object.entries(this.groupedMetricsHistory).forEach(([serverId, records]) => {
            // 渲染历史页面的大图表
            this.renderSingleChart(serverId, records, `metrics-chart-${serverId}`);
            // 同时尝试渲染卡片图表 (如果 DOM 存在)
            this.renderSingleChart(serverId, records, `metrics-chart-card-${serverId}`);
        });
    },

    /**
     * 渲染单个指标图表
     * @param {string} serverId 主机 ID
     * @param {Array} records 历史记录数据
     * @param {string} canvasId Canvas 元素 ID
     */
    async renderSingleChart(serverId, records, canvasId) {
        // 确保 Chart.js 已加载，否则触发回退加载
        if (!window.Chart) {
            const loaded = await this.loadChartJsFallback();
            if (!loaded) return;
        }
        if (!records || records.length === 0) return;

        const canvas = document.getElementById(canvasId);
        if (!canvas) return;

        // 由于记录通常是记录时间倒序排列的，绘图前先克隆并正序排列
        let sortedRecords = [...records].sort((a, b) => new Date(a.recorded_at) - new Date(b.recorded_at));

        // 性能优化：数据点过多时进行降采样 (最多保留 100 个点)
        const MAX_POINTS = 50;
        if (sortedRecords.length > MAX_POINTS) {
            const step = Math.ceil(sortedRecords.length / MAX_POINTS);
            sortedRecords = sortedRecords.filter((_, index) => index % step === 0);
        }

        // 准备数据
        const labels = sortedRecords.map(r => {
            const d = new Date(r.recorded_at);
            return d.getHours() + ':' + String(d.getMinutes()).padStart(2, '0');
        });
        const cpuData = sortedRecords.map(r => r.cpu_usage || 0);
        const memData = sortedRecords.map(r => r.mem_usage || 0);

        // 销毁已存在的实例
        const existingChart = Chart.getChart(canvas);
        if (existingChart) {
            existingChart.destroy();
        }

        // 创建新图表
        new Chart(canvas, {
            type: 'line',
            data: {
                labels: labels,
                datasets: [
                    {
                        label: 'CPU (%)',
                        data: cpuData,
                        borderColor: '#10b981',
                        backgroundColor: 'transparent',
                        borderWidth: 2.5,
                        fill: false,
                        tension: 0.3,
                        pointRadius: 0,
                        pointHoverRadius: 5
                    },
                    {
                        label: '内存 (%)',
                        data: memData,
                        borderColor: '#3b82f6',
                        backgroundColor: 'transparent',
                        borderWidth: 2.5,
                        fill: false,
                        tension: 0.3,
                        pointRadius: 0,
                        pointHoverRadius: 5
                    }
                ]
            },
            options: {
                responsive: true,
                maintainAspectRatio: false,
                plugins: {
                    legend: { display: false },
                    tooltip: {
                        mode: 'index',
                        intersect: false,
                        padding: 10,
                        backgroundColor: 'rgba(13, 17, 23, 0.9)',
                        titleColor: '#8b949e',
                        bodyColor: '#e6edf3',
                        borderColor: 'rgba(255, 255, 255, 0.1)',
                        borderWidth: 1
                    }
                },
                scales: {
                    x: {
                        display: true,
                        grid: {
                            display: true,
                            color: 'rgba(255, 255, 255, 0.06)',
                            drawBorder: false
                        },
                        ticks: {
                            maxRotation: 0,
                            autoSkip: true,
                            maxTicksLimit: 6,
                            font: { size: 10 },
                            color: '#6e7681'
                        }
                    },
                    y: {
                        display: true,
                        min: 0,
                        max: 100,
                        grid: {
                            display: true,
                            color: 'rgba(255, 255, 255, 0.06)',
                            drawBorder: false
                        },
                        ticks: {
                            font: { size: 10 },
                            color: '#6e7681',
                            stepSize: 25
                        }
                    }
                },
                interaction: {
                    mode: 'nearest',
                    axis: 'x',
                    intersect: false
                }
            }
        });
    },

    /**
     * 为特定主机加载指标历史数据（用于卡片展示）
     */
    async loadCardMetrics(serverId) {
        if (!serverId) return;

        try {
            // 计算时间范围 (使用与 loadMetricsHistory 相同的逻辑)
            let startTime = null;
            const now = Date.now();

            switch (this.metricsHistoryTimeRange) {
                case '1h': startTime = new Date(now - 60 * 60 * 1000).toISOString(); break;
                case '6h': startTime = new Date(now - 6 * 60 * 60 * 1000).toISOString(); break;
                case '24h': startTime = new Date(now - 24 * 60 * 60 * 1000).toISOString(); break;
                case '7d': startTime = new Date(now - 7 * 24 * 60 * 60 * 1000).toISOString(); break;
                case 'all': default: startTime = null;
            }

            const params = new URLSearchParams({
                serverId: serverId,
                page: 1,
                pageSize: 10000 // 获取该段时间内所有记录以保证图表精细度
            });

            if (startTime) {
                params.append('startTime', startTime);
            }

            const response = await fetch(`/api/server/metrics/history?${params}`, {
                headers: this.getAuthHeaders()
            });
            const data = await response.json();

            if (data.success && data.data.length > 0) {
                // 如果当前已经在 history 列表里了，则合并，否则直接放入
                // 为了简单起见，我们直接看 groupedMetricsHistory 能否拿到
                // 我们把获取到的数据放入一个临时的缓存或直接渲染
                const records = data.data;

                this.$nextTick(() => {
                    this.renderSingleChart(serverId, records, `metrics-chart-card-${serverId}`);
                });
            }
        } catch (error) {
            console.error('加载卡片指标失败:', error);
        }
    },

    // ==================== 采集器管理 ====================

    async loadCollectorStatus() {
        try {
            const response = await fetch('/api/server/metrics/collector/status', {
                headers: this.getAuthHeaders()
            });
            const data = await response.json();

            if (data.success) {
                this.metricsCollectorStatus = data.data;
                if (data.data.interval) {
                    this.metricsCollectInterval = Math.floor(data.data.interval / 60000);
                }
            }
        } catch (error) {
            console.error('加载采集器状态失败:', error);
        }
    },

    getCpuClass(usage) {
        if (!usage && usage !== 0) return '';
        const val = parseFloat(usage);
        if (val >= 90) return 'critical';
        if (val >= 70) return 'warning';
        return 'normal';
    },

    toggleMetricsServerExpand(serverId) {
        const index = this.expandedMetricsServers.indexOf(serverId);
        if (index === -1) {
            this.expandedMetricsServers.push(serverId);
        } else {
            this.expandedMetricsServers.splice(index, 1);
        }
    },

    async updateMetricsCollectInterval() {
        try {
            const intervalMs = this.metricsCollectInterval * 60 * 1000;
            const response = await fetch('/api/server/metrics/collector/interval', {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ interval: intervalMs })
            });
            const data = await response.json();

            if (data.success) {
                this.showGlobalToast(`采集间隔已更新为 ${this.metricsCollectInterval} 分钟`, 'success');
                this.loadCollectorStatus();
            } else {
                this.showGlobalToast('更新失败: ' + data.error, 'error');
            }
        } catch (error) {
            console.error('更新采集间隔失败:', error);
            this.showGlobalToast('更新采集间隔失败', 'error');
        }
    },

    /**
     * 加载监控配置
     */
    async loadMonitorConfig() {
        try {
            const response = await fetch('/api/server/monitor/config', {
                headers: this.getAuthHeaders()
            });
            const data = await response.json();
            if (data.success) {
                this.monitorConfig = data.data;
                // 同步更新显示用的采集间隔
                if (data.data.metrics_collect_interval) {
                    this.metricsCollectInterval = Math.floor(data.data.metrics_collect_interval / 60);
                }
                // 加载采集器运行状态
                this.loadCollectorStatus();
            }
        } catch (error) {
            console.error('加载监控配置失败:', error);
        }
    },

    /**
     * 更新监控全局配置
     */
    async updateMonitorConfig() {
        try {
            const response = await fetch('/api/server/monitor/config', {
                method: 'PUT',
                headers: {
                    ...this.getAuthHeaders(),
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify(this.monitorConfig)
            });
            const data = await response.json();
            if (data.success) {
                this.showGlobalToast('配置已更新', 'success');
                this.loadCollectorStatus();
                // 重新加载配置以确保同步
                this.loadMonitorConfig();
            }
        } catch (error) {
            this.showGlobalToast('配置更新失败', 'error');
            console.error('更新配置失败:', error);
        }
    }
};
