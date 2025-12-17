/**
 * 服务器管理模块路由
 */

const express = require('express');
const router = express.Router();
const { serverStorage, monitorLogStorage, monitorConfigStorage } = require('./storage');
const sshService = require('./ssh-service');
const sftpService = require('./sftp-service');
const monitorService = require('./monitor-service');
const systemInfoService = require('./system-info-service');

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
 * 注意：必须在 /accounts/:id 之前定义，否则 export 会被当作 id
 */
router.get('/accounts/export', (req, res) => {
    try {
        const servers = serverStorage.getAll();

        // 移除敏感信息（密码、私钥）
        const exportData = servers.map(server => ({
            name: server.name,
            host: server.host,
            port: server.port,
            username: server.username,
            auth_type: server.auth_type,
            tags: server.tags,
            description: server.description
        }));

        res.json({
            success: true,
            data: exportData
        });
    } catch (error) {
        res.status(500).json({
            success: false,
            error: error.message
        });
    }
});

/**
 * 获取单个服务器
 */
router.get('/accounts/:id', (req, res) => {
    try {
        const server = serverStorage.getById(req.params.id);

        if (!server) {
            return res.status(404).json({
                success: false,
                error: '服务器不存在'
            });
        }

        res.json({
            success: true,
            data: server
        });
    } catch (error) {
        res.status(500).json({
            success: false,
            error: error.message
        });
    }
});

/**
 * 添加服务器
 */
router.post('/accounts', (req, res) => {
    try {
        const { name, host, port, username, auth_type, password, private_key, passphrase, tags, description } = req.body;

        // 验证必填字段
        if (!name || !host || !username || !auth_type) {
            return res.status(400).json({
                success: false,
                error: '缺少必填字段'
            });
        }

        // 验证认证方式
        if (auth_type === 'password' && !password) {
            return res.status(400).json({
                success: false,
                error: '密码认证需要提供密码'
            });
        }

        if (auth_type === 'key' && !private_key) {
            return res.status(400).json({
                success: false,
                error: '密钥认证需要提供私钥'
            });
        }

        const server = serverStorage.create({
            name,
            host,
            port: port || 22,
            username,
            auth_type,
            password,
            private_key,
            passphrase,
            tags,
            description
        });

        res.json({
            success: true,
            message: '服务器添加成功',
            data: server
        });
    } catch (error) {
        res.status(500).json({
            success: false,
            error: error.message
        });
    }
});

/**
 * 更新服务器
 */
router.put('/accounts/:id', (req, res) => {
    try {
        const server = serverStorage.update(req.params.id, req.body);

        if (!server) {
            return res.status(404).json({
                success: false,
                error: '服务器不存在'
            });
        }

        res.json({
            success: true,
            message: '服务器更新成功',
            data: server
        });
    } catch (error) {
        res.status(500).json({
            success: false,
            error: error.message
        });
    }
});

/**
 * 删除服务器
 */
router.delete('/accounts/:id', (req, res) => {
    try {
        const success = serverStorage.delete(req.params.id);

        if (!success) {
            return res.status(404).json({
                success: false,
                error: '服务器不存在'
            });
        }

        // 关闭该服务器的 SSH 连接
        sshService.closeConnection(req.params.id);

        res.json({
            success: true,
            message: '服务器删除成功'
        });
    } catch (error) {
        res.status(500).json({
            success: false,
            error: error.message
        });
    }
});

/**
 * 批量删除服务器
 */
router.post('/accounts/batch-delete', (req, res) => {
    try {
        const { ids } = req.body;

        if (!ids || !Array.isArray(ids) || ids.length === 0) {
            return res.status(400).json({
                success: false,
                error: '请提供要删除的服务器 ID 列表'
            });
        }

        const count = serverStorage.deleteMany(ids);

        // 关闭这些服务器的 SSH 连接
        ids.forEach(id => sshService.closeConnection(id));

        res.json({
            success: true,
            message: `成功删除 ${count} 台服务器`
        });
    } catch (error) {
        res.status(500).json({
            success: false,
            error: error.message
        });
    }
});

/**
 * 批量导入服务器
 */
router.post('/accounts/import', (req, res) => {
    try {
        const { servers } = req.body;

        if (!servers || !Array.isArray(servers)) {
            return res.status(400).json({
                success: false,
                error: '请提供服务器列表'
            });
        }

        const results = [];
        let successCount = 0;
        let failedCount = 0;

        servers.forEach(serverData => {
            try {
                const server = serverStorage.create(serverData);
                results.push({
                    success: true,
                    data: server
                });
                successCount++;
            } catch (error) {
                results.push({
                    success: false,
                    error: error.message,
                    data: serverData
                });
                failedCount++;
            }
        });

        res.json({
            success: true,
            message: `导入完成: 成功 ${successCount}, 失败 ${failedCount}`,
            results
        });
    } catch (error) {
        res.status(500).json({
            success: false,
            error: error.message
        });
    }
});

// ==================== 服务器操作接口 ====================

/**
 * 测试服务器连接
 */
router.post('/test-connection', async (req, res) => {
    try {
        const serverConfig = req.body;

        if (!serverConfig.host || !serverConfig.username) {
            return res.status(400).json({
                success: false,
                error: '缺少必填字段'
            });
        }

        const result = await sshService.testConnection(serverConfig);

        res.json(result);
    } catch (error) {
        res.status(500).json({
            success: false,
            error: error.message
        });
    }
});

/**
 * 手动触发探测所有服务器
 */
router.post('/check-all', async (req, res) => {
    try {
        const result = await monitorService.manualProbeAll();
        res.json(result);
    } catch (error) {
        res.status(500).json({
            success: false,
            error: error.message
        });
    }
});

/**
 * 获取服务器详细信息
 */
router.post('/info', async (req, res) => {
    try {
        const { serverId } = req.body;

        if (!serverId) {
            return res.status(400).json({
                success: false,
                error: '缺少服务器 ID'
            });
        }

        const server = serverStorage.getById(serverId);

        if (!server) {
            return res.status(404).json({
                success: false,
                error: '服务器不存在'
            });
        }

        const info = await systemInfoService.getServerInfo(serverId, server);

        res.json(info);
    } catch (error) {
        res.status(500).json({
            success: false,
            error: error.message
        });
    }
});

/**
 * 执行服务器操作（重启/关机）
 */
router.post('/action', async (req, res) => {
    try {
        const { serverId, action } = req.body;

        if (!serverId || !action) {
            return res.status(400).json({
                success: false,
                error: '缺少必填字段'
            });
        }

        const server = serverStorage.getById(serverId);

        if (!server) {
            return res.status(404).json({
                success: false,
                error: '服务器不存在'
            });
        }

        const result = await systemInfoService.executeServerAction(serverId, server, action);

        res.json(result);
    } catch (error) {
        res.status(500).json({
            success: false,
            error: error.message
        });
    }
});

// ==================== SSH 终端接口 ====================

/**
 * 执行 SSH 命令
 */
router.post('/ssh/exec', async (req, res) => {
    try {
        const { serverId, command } = req.body;

        if (!serverId || !command) {
            return res.status(400).json({
                success: false,
                error: '缺少必填字段'
            });
        }

        const server = serverStorage.getById(serverId);

        if (!server) {
            return res.status(404).json({
                success: false,
                error: '服务器不存在'
            });
        }

        const result = await sshService.executeCommand(serverId, server, command);

        res.json(result);
    } catch (error) {
        res.status(500).json({
            success: false,
            error: error.message
        });
    }
});

/**
 * 关闭 SSH 连接
 */
router.post('/ssh/disconnect', (req, res) => {
    try {
        const { serverId } = req.body;

        if (!serverId) {
            return res.status(400).json({
                success: false,
                error: '缺少服务器 ID'
            });
        }

        sshService.closeConnection(serverId);

        res.json({
            success: true,
            message: 'SSH 连接已关闭'
        });
    } catch (error) {
        res.status(500).json({
            success: false,
            error: error.message
        });
    }
});

/**
 * 获取 SSH 连接池状态
 */
router.get('/ssh/status', (req, res) => {
    try {
        const status = sshService.getStatus();
        res.json({
            success: true,
            data: status
        });
    } catch (error) {
        res.status(500).json({
            success: false,
            error: error.message
        });
    }
});

// ==================== SFTP 文件管理接口 ====================

/**
 * 列出目录内容
 */
router.post('/sftp/list', async (req, res) => {
    try {
        const { serverId, path } = req.body;

        if (!serverId) {
            return res.status(400).json({
                success: false,
                error: '缺少服务器 ID'
            });
        }

        const server = serverStorage.getById(serverId);

        if (!server) {
            return res.status(404).json({
                success: false,
                error: '服务器不存在'
            });
        }

        const result = await sftpService.listDirectory(server, path || '/');

        res.json(result);
    } catch (error) {
        res.status(500).json({
            success: false,
            error: error.message
        });
    }
});

/**
 * 上传文件
 */
router.post('/sftp/upload', async (req, res) => {
    try {
        const { serverId, localPath, remotePath } = req.body;

        if (!serverId || !localPath || !remotePath) {
            return res.status(400).json({
                success: false,
                error: '缺少必填字段'
            });
        }

        const server = serverStorage.getById(serverId);

        if (!server) {
            return res.status(404).json({
                success: false,
                error: '服务器不存在'
            });
        }

        const result = await sftpService.uploadFile(server, localPath, remotePath);

        res.json(result);
    } catch (error) {
        res.status(500).json({
            success: false,
            error: error.message
        });
    }
});

/**
 * 下载文件
 */
router.post('/sftp/download', async (req, res) => {
    try {
        const { serverId, remotePath, localPath } = req.body;

        if (!serverId || !remotePath || !localPath) {
            return res.status(400).json({
                success: false,
                error: '缺少必填字段'
            });
        }

        const server = serverStorage.getById(serverId);

        if (!server) {
            return res.status(404).json({
                success: false,
                error: '服务器不存在'
            });
        }

        const result = await sftpService.downloadFile(server, remotePath, localPath);

        res.json(result);
    } catch (error) {
        res.status(500).json({
            success: false,
            error: error.message
        });
    }
});

/**
 * 删除文件或目录
 */
router.post('/sftp/delete', async (req, res) => {
    try {
        const { serverId, path, isDirectory } = req.body;

        if (!serverId || !path) {
            return res.status(400).json({
                success: false,
                error: '缺少必填字段'
            });
        }

        const server = serverStorage.getById(serverId);

        if (!server) {
            return res.status(404).json({
                success: false,
                error: '服务器不存在'
            });
        }

        const result = await sftpService.deleteFile(server, path, isDirectory);

        res.json(result);
    } catch (error) {
        res.status(500).json({
            success: false,
            error: error.message
        });
    }
});

/**
 * 重命名文件或目录
 */
router.post('/sftp/rename', async (req, res) => {
    try {
        const { serverId, oldPath, newPath } = req.body;

        if (!serverId || !oldPath || !newPath) {
            return res.status(400).json({
                success: false,
                error: '缺少必填字段'
            });
        }

        const server = serverStorage.getById(serverId);

        if (!server) {
            return res.status(404).json({
                success: false,
                error: '服务器不存在'
            });
        }

        const result = await sftpService.renameFile(server, oldPath, newPath);

        res.json(result);
    } catch (error) {
        res.status(500).json({
            success: false,
            error: error.message
        });
    }
});

/**
 * 创建目录
 */
router.post('/sftp/mkdir', async (req, res) => {
    try {
        const { serverId, path, recursive } = req.body;

        if (!serverId || !path) {
            return res.status(400).json({
                success: false,
                error: '缺少必填字段'
            });
        }

        const server = serverStorage.getById(serverId);

        if (!server) {
            return res.status(404).json({
                success: false,
                error: '服务器不存在'
            });
        }

        const result = await sftpService.createDirectory(server, path, recursive !== false);

        res.json(result);
    } catch (error) {
        res.status(500).json({
            success: false,
            error: error.message
        });
    }
});

// ==================== 监控配置接口 ====================

/**
 * 获取监控配置
 */
router.get('/monitor/config', (req, res) => {
    try {
        const config = monitorConfigStorage.get();
        res.json({
            success: true,
            data: config
        });
    } catch (error) {
        res.status(500).json({
            success: false,
            error: error.message
        });
    }
});

/**
 * 更新监控配置
 */
router.put('/monitor/config', (req, res) => {
    try {
        const config = monitorConfigStorage.update(req.body);

        // 重启监控服务以应用新配置
        monitorService.restart();

        res.json({
            success: true,
            message: '监控配置更新成功',
            data: config
        });
    } catch (error) {
        res.status(500).json({
            success: false,
            error: error.message
        });
    }
});

/**
 * 获取监控日志
 */
router.get('/monitor/logs', (req, res) => {
    try {
        const { serverId, status, limit, offset } = req.query;

        const options = {
            serverId,
            status,
            limit: parseInt(limit) || 100,
            offset: parseInt(offset) || 0
        };

        const logs = monitorLogStorage.getAll(options);
        const total = monitorLogStorage.getCount({ serverId, status });

        res.json({
            success: true,
            data: {
                logs,
                total,
                limit: options.limit,
                offset: options.offset
            }
        });
    } catch (error) {
        res.status(500).json({
            success: false,
            error: error.message
        });
    }
});

/**
 * 获取监控服务状态
 */
router.get('/monitor/status', (req, res) => {
    try {
        const status = monitorService.getStatus();
        res.json({
            success: true,
            data: status
        });
    } catch (error) {
        res.status(500).json({
            success: false,
            error: error.message
        });
    }
});

/**
 * 启动监控服务
 */
router.post('/monitor/start', (req, res) => {
    try {
        monitorService.start();
        res.json({
            success: true,
            message: '监控服务已启动'
        });
    } catch (error) {
        res.status(500).json({
            success: false,
            error: error.message
        });
    }
});

/**
 * 停止监控服务
 */
router.post('/monitor/stop', (req, res) => {
    try {
        monitorService.stop();
        res.json({
            success: true,
            message: '监控服务已停止'
        });
    } catch (error) {
        res.status(500).json({
            success: false,
            error: error.message
        });
    }
});

module.exports = router;
