/**
 * 服务器管理模块路由
 */

const express = require('express');
const router = express.Router();
const { serverStorage, monitorLogStorage, monitorConfigStorage, snippetStorage } = require('./storage');
const sshService = require('./ssh-service');
const monitorService = require('./monitor-service');
const systemInfoService = require('./system-info-service');

// ==================== 主机凭据接口 ====================

// 挂载凭据管理路由 (位置靠前以避免被模糊路由拦截)
const credentialsRouter = require('./credentials-router');
router.use('/credentials', credentialsRouter);

// ==================== 服务器管理接口 ====================

/**
 * 获取所有服务器
 */
router.get('/accounts', (req, res) => {
    try {
        const servers = serverStorage.getAll();
        res.json({
            success: true,
            data: servers
        });
    } catch (error) {
        res.status(500).json({
            success: false,
            error: error.message
        });
    }
});

/**
 * 批量导出服务器
 */
router.get('/accounts/export', (req, res) => {
    try {
        const servers = serverStorage.getAll();
        const exportData = servers.map(server => ({
            name: server.name,
            host: server.host,
            port: server.port,
            username: server.username,
            auth_type: server.auth_type,
            password: server.password,
            private_key: server.private_key,
            passphrase: server.passphrase,
            tags: server.tags,
            description: server.description
        }));
        res.json({ success: true, data: exportData });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 获取单个服务器
 */
router.get('/accounts/:id', (req, res) => {
    try {
        const server = serverStorage.getById(req.params.id);
        if (!server) return res.status(404).json({ success: false, error: '服务器不存在' });
        res.json({ success: true, data: server });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 批量导入服务器
 */
router.post('/accounts/import', (req, res) => {
    try {
        const { servers } = req.body;
        if (!servers || !Array.isArray(servers)) {
            return res.status(400).json({ success: false, error: '请提供服务器列表' });
        }
        const results = [];
        let successCount = 0;
        let failedCount = 0;
        servers.forEach(serverData => {
            try {
                const server = serverStorage.create(serverData);
                results.push({ success: true, data: server });
                successCount++;
            } catch (error) {
                results.push({ success: false, error: error.message, data: serverData });
                failedCount++;
            }
        });
        res.json({
            success: true,
            message: `导入完成: 成功 ${successCount}, 失败 ${failedCount}`,
            results
        });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 添加服务器
 */
router.post('/accounts', (req, res) => {
    try {
        const { name, host, port, username, auth_type, password, private_key, passphrase, tags, description } = req.body;
        if (!name || !host || !username || !auth_type) {
            return res.status(400).json({ success: false, error: '缺少必填字段' });
        }
        const server = serverStorage.create({
            name, host, port: port || 22, username, auth_type,
            password, private_key, passphrase, tags, description
        });
        res.json({ success: true, message: '服务器添加成功', data: server });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 更新服务器
 */
router.put('/accounts/:id', (req, res) => {
    try {
        const server = serverStorage.update(req.params.id, req.body);
        if (!server) return res.status(404).json({ success: false, error: '服务器不存在' });
        res.json({ success: true, message: '服务器更新成功', data: server });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 删除服务器
 */
router.delete('/accounts/:id', (req, res) => {
    try {
        const success = serverStorage.delete(req.params.id);
        if (!success) return res.status(404).json({ success: false, error: '服务器不存在' });
        sshService.closeConnection(req.params.id);
        res.json({ success: true, message: '服务器删除成功' });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 测试连接
 */
router.post('/test-connection', async (req, res) => {
    try {
        const serverConfig = req.body;
        // 临时生成一个虚拟 ID 用于连接尝试
        const tempId = `test_${Date.now()}`;

        // 尝试执行一个简单的命令
        const result = await sshService.executeCommand(tempId, serverConfig, 'echo "SSH_OK"', 0);

        // 测试完后立即关闭这个临时连接，不占用线程池
        sshService.closeConnection(tempId);

        if (result.success) {
            res.json({ success: true, message: '连接成功' });
        } else {
            res.json({ success: false, error: result.error || '连接失败' });
        }
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

// ==================== 服务器操作接口 ====================

/**
 * 手动触发探测所有服务器
 */
router.post('/check-all', async (req, res) => {
    try {
        const result = await monitorService.manualProbeAll();
        res.json(result);
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 批量 TCP ping 测量延迟
 */
router.post('/ping-all', async (req, res) => {
    try {
        const servers = ServerAccount.getAll();
        const results = [];

        // 并发 ping 所有主机
        await Promise.all(servers.map(async (server) => {
            try {
                const latency = await metricsService.tcpPing(server.host, server.port || 22);
                // 更新数据库
                ServerAccount.updateStatus(server.id, { response_time: latency });
                results.push({ serverId: server.id, latency, success: true });
            } catch (error) {
                results.push({ serverId: server.id, latency: null, success: false, error: error.message });
            }
        }));

        res.json({ success: true, results });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 获取服务器详细信息 (极速缓存优化)
 */
router.post('/info', async (req, res) => {
    try {
        const { serverId, force } = req.body;
        if (!serverId) return res.status(400).json({ success: false, error: '缺少服务器 ID' });

        if (!force) {
            const cachedMetrics = monitorService.getMetrics(serverId);
            if (cachedMetrics) return res.json({ ...cachedMetrics, is_cached: true });
        }

        const server = serverStorage.getById(serverId);
        if (!server) return res.status(404).json({ success: false, error: '服务器不存在' });

        const info = await systemInfoService.getServerInfo(serverId, server);
        if (info.success) {
            monitorService.metricsCache.set(serverId, { ...info, cached_at: new Date().toISOString() });
        }
        res.json(info);
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * Docker 容器操作
 */
router.post('/docker/action', async (req, res) => {
    try {
        const { serverId, containerId, action } = req.body;
        if (!serverId || !containerId || !action) {
            return res.status(400).json({ success: false, error: '缺少必填参数' });
        }

        const server = serverStorage.getById(serverId);
        if (!server) return res.status(404).json({ success: false, error: '服务器不存在' });

        // 构建 Docker 命令
        let command = '';
        switch (action) {
            case 'start': command = `docker start ${containerId}`; break;
            case 'stop': command = `docker stop ${containerId}`; break;
            case 'restart': command = `docker restart ${containerId}`; break;
            case 'pause': command = `docker pause ${containerId}`; break;
            case 'unpause': command = `docker unpause ${containerId}`; break;
            case 'remove': command = `docker rm -f ${containerId}`; break;
            default:
                return res.status(400).json({ success: false, error: '不支持的操作类型' });
        }

        const result = await sshService.executeCommand(serverId, server, command);
        res.json({
            success: result.success,
            message: result.success ? '操作执行成功' : '操作执行失败',
            output: result.stdout || result.stderr
        });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

// ==================== SSH 终端接口 ====================

router.post('/ssh/exec', async (req, res) => {
    try {
        const { serverId, command } = req.body;
        const server = serverStorage.getById(serverId);
        if (!server) return res.status(404).json({ success: false, error: '服务器不存在' });
        const result = await sshService.executeCommand(serverId, server, command);
        res.json(result);
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

router.post('/ssh/disconnect', (req, res) => {
    try {
        const { serverId } = req.body;
        sshService.closeConnection(serverId);
        res.json({ success: true, message: 'SSH 连接已关闭' });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

// ==================== 代码片段接口 ====================

router.get('/snippets', (req, res) => {
    try {
        const snippets = snippetStorage.getAll();
        res.json({ success: true, data: snippets });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

router.post('/snippets', (req, res) => {
    try {
        const snippet = snippetStorage.create(req.body);
        res.json({ success: true, data: snippet });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

router.put('/snippets/:id', (req, res) => {
    try {
        const success = snippetStorage.update(req.params.id, req.body);
        res.json({ success: true, data: success });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

router.delete('/snippets/:id', (req, res) => {
    try {
        const success = snippetStorage.delete(req.params.id);
        res.json({ success: true, data: success });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

// ==================== 监控配置接口 ====================

router.get('/monitor/config', (req, res) => {
    try {
        const config = monitorConfigStorage.get();
        res.json({ success: true, data: config });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

router.put('/monitor/config', (req, res) => {
    try {
        const config = monitorConfigStorage.update(req.body);
        monitorService.restart();
        res.json({ success: true, message: '监控配置更新成功', data: config });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

// ==================== 历史指标接口 ====================

const { ServerMetricsHistory } = require('./models');
const metricsService = require('./metrics-service');

/**
 * 获取历史指标记录
 */
router.get('/metrics/history', (req, res) => {
    try {
        const {
            serverId,
            startTime,
            endTime,
            page = 1,
            pageSize = 50
        } = req.query;

        const limit = Math.min(parseInt(pageSize) || 50, 200);
        const offset = ((parseInt(page) || 1) - 1) * limit;

        const records = ServerMetricsHistory.getHistory({
            serverId: serverId || null,
            startTime: startTime || null,
            endTime: endTime || null,
            limit,
            offset
        });

        const total = ServerMetricsHistory.getCount({
            serverId: serverId || null,
            startTime: startTime || null,
            endTime: endTime || null
        });

        res.json({
            success: true,
            data: records,
            pagination: {
                page: parseInt(page) || 1,
                pageSize: limit,
                total,
                totalPages: Math.ceil(total / limit)
            }
        });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 获取指定主机的统计数据
 */
router.get('/metrics/stats/:serverId', (req, res) => {
    try {
        const { serverId } = req.params;
        const { hours = 24 } = req.query;

        const stats = ServerMetricsHistory.getStats(serverId, parseInt(hours) || 24);
        res.json({ success: true, data: stats });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 获取采集器状态
 */
router.get('/metrics/collector/status', (req, res) => {
    try {
        const status = metricsService.getCollectorStatus();
        res.json({ success: true, data: status });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 手动触发一次历史采集
 */
router.post('/metrics/collect', (req, res) => {
    try {
        metricsService.collectHistorySnapshot();
        res.json({ success: true, message: '已触发历史指标采集' });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 更新采集间隔
 */
router.put('/metrics/collector/interval', (req, res) => {
    try {
        const { interval } = req.body;
        if (!interval || interval < 60000) {
            return res.status(400).json({ success: false, error: '采集间隔至少为 1 分钟' });
        }
        metricsService.setCollectInterval(interval);
        res.json({ success: true, message: '采集间隔已更新' });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 清理过期历史记录
 */
router.delete('/metrics/history/cleanup', (req, res) => {
    try {
        const { days = 7 } = req.query;
        const deleted = ServerMetricsHistory.deleteOldRecords(parseInt(days) || 7);
        res.json({ success: true, message: `已清理 ${deleted} 条过期记录` });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

module.exports = router;