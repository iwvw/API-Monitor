/**
 * 服务器管理模块路由
 */

const express = require('express');
const router = express.Router();
const {
  serverStorage,
  monitorLogStorage,
  monitorConfigStorage,
  snippetStorage,
} = require('./storage');
const monitorService = require('./monitor-service');
const agentService = require('./agent-service');
const sshService = require('./ssh-service');
const { ServerMonitorConfig, ServerMetricsHistory } = require('./models');
const { TaskTypes: DockerTaskTypes } = require('./protocol');

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

    // 附带后端缓存的最新指标（通过 agentService 获取）
    const serversWithMetrics = servers.map(server => {
      const cachedMetrics = agentService.getMetrics(server.id);
      const isOnline = agentService.isOnline(server.id);

      if (cachedMetrics) {
        // 解析 disk 字符串为结构化对象 (格式: "38G/40G (95%)")
        let diskArray = [];
        if (cachedMetrics.disk && typeof cachedMetrics.disk === 'string') {
          const diskMatch = cachedMetrics.disk.match(/([^/]+)\/([^\s]+)\s\((\d+\.?\d*%?)\)/);
          if (diskMatch) {
            diskArray = [
              {
                device: '/',
                used: diskMatch[1],
                total: diskMatch[2],
                usage: diskMatch[3],
              },
            ];
          }
        }

        return {
          ...server,
          status: isOnline ? 'online' : server.status || 'offline', // 动态设置在线状态
          info: {
            cpu: {
              Load: cachedMetrics.load,
              Cores: cachedMetrics.cores,
              Usage: cachedMetrics.cpu_usage,
            },
            memory: {
              Usage:
                cachedMetrics.mem_percent !== undefined
                  ? Math.round(cachedMetrics.mem_percent) + '%'
                  : '-',
              Used: cachedMetrics.mem ? cachedMetrics.mem.split('/')[0] : '-',
              Total: cachedMetrics.mem ? cachedMetrics.mem.split('/')[1] : '-',
            },
            disk: diskArray,
            docker: cachedMetrics.docker,
            network: cachedMetrics.network,
            gpu: {
              Model: cachedMetrics.gpu_model,
              Usage: cachedMetrics.gpu_usage,
              Memory: cachedMetrics.gpu_mem,
              Power: cachedMetrics.gpu_power,
            },
            platform: cachedMetrics.platform,
            platformVersion: cachedMetrics.platformVersion,
            agentVersion: cachedMetrics.agent_version,
            uptime: cachedMetrics.uptime,
            lastUpdate:
              cachedMetrics.lastUpdate ||
              (cachedMetrics.timestamp
                ? new Date(cachedMetrics.timestamp).toLocaleTimeString()
                : '-'),
          },
        };
      }
      // 没有缓存指标时，根据 Agent 连接状态判断
      return {
        ...server,
        status: isOnline ? 'online' : server.status || 'offline',
      };
    });

    res.json({
      success: true,
      data: serversWithMetrics,
    });
  } catch (error) {
    res.status(500).json({
      success: false,
      error: error.message,
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
      description: server.description,
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
      results,
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
    const {
      name,
      host,
      port,
      username,
      auth_type,
      password,
      private_key,
      passphrase,
      tags,
      description,
    } = req.body;
    if (!name || !host || !username || !auth_type) {
      return res.status(400).json({ success: false, error: '缺少必填字段' });
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
      description,
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
 * 获取监控日志 (带分页)
 */
router.get('/monitor/logs', (req, res) => {
  try {
    const { serverId, status, page = 1, pageSize = 50 } = req.query;
    const limit = parseInt(pageSize);
    const offset = (parseInt(page) - 1) * limit;

    const logs = monitorLogStorage.getAll({
      serverId: serverId || null,
      status: status || null,
      limit,
      offset,
    });

    const total = monitorLogStorage.getCount({
      serverId: serverId || null,
      status: status || null,
    });

    res.json({
      success: true,
      data: logs,
      pagination: {
        total,
        page: parseInt(page),
        pageSize: limit,
        totalPages: Math.ceil(total / limit),
      },
    });
  } catch (error) {
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * 批量 TCP ping 测量延迟
 */
router.post('/ping-all', async (req, res) => {
  try {
    const servers = serverStorage.getAll();
    const results = [];

    // 并发 ping 所有主机
    await Promise.all(
      servers.map(async server => {
        try {
          const latency = await monitorService.tcpPing(server.host, server.port || 22);
          // 更新数据库
          serverStorage.updateStatus(server.id, { response_time: latency });
          results.push({ serverId: server.id, latency, success: true });
        } catch (error) {
          results.push({
            serverId: server.id,
            latency: null,
            success: false,
            error: error.message,
          });
        }
      })
    );

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

    const server = serverStorage.getById(serverId);
    if (!server) return res.status(404).json({ success: false, error: '服务器不存在' });

    // 纯 Agent 模式：直接返回内存中的最新指标作为服务器详情
    const metrics = agentService.getMetrics(serverId);
    if (!metrics) {
      return res
        .status(404)
        .json({ success: false, error: 'Agent 指标尚未就绪，请确保 Agent 已启动并在线' });
    }

    res.json({
      success: true,
      ...metrics,
      is_agent: true,
    });
  } catch (error) {
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * Docker 容器操作
 * POST /docker/action
 * { serverId, containerId, action: 'start'|'stop'|'restart'|'pause'|'unpause'|'update'|'pull', image?: string }
 */
const { TaskTypes: DockerTaskTypes } = require('./protocol');

router.post('/docker/action', async (req, res) => {
  try {
    const { serverId, containerId, action, image } = req.body;

    if (!serverId) {
      return res.status(400).json({ success: false, error: '缺少服务器 ID' });
    }
    if (!containerId) {
      return res.status(400).json({ success: false, error: '缺少容器 ID' });
    }
    if (!action) {
      return res.status(400).json({ success: false, error: '缺少操作类型' });
    }

    const validActions = ['start', 'stop', 'restart', 'pause', 'unpause', 'update', 'pull'];
    if (!validActions.includes(action)) {
      return res.status(400).json({ success: false, error: `不支持的操作: ${action}` });
    }

    // 检查主机是否在线
    if (!agentService.isOnline(serverId)) {
      return res.status(400).json({ success: false, error: '主机不在线' });
    }

    // 构建任务数据
    const taskData = JSON.stringify({
      action,
      container_id: containerId,
      image: image || '',
    });

    // 发送任务并等待结果 (Docker 操作可能较慢，给 2 分钟超时)
    const result = await agentService.sendTaskAndWait(
      serverId,
      {
        type: DockerTaskTypes.DOCKER_ACTION,
        data: taskData,
        timeout: 120,
      },
      120000
    );

    if (result.successful) {
      res.json({
        success: true,
        message: result.data || `${action} 操作成功`,
        data: {
          serverId,
          containerId,
          action,
          result: result.data,
        },
      });
    } else {
      res.status(400).json({
        success: false,
        error: result.data || `${action} 操作失败`,
      });
    }
  } catch (error) {
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * Docker 镜像更新检测
 * POST /docker/check-update
 * { serverId, containerId?: string }
 * 
 * 检查指定容器（或所有运行中的容器）的镜像是否有更新可用
 * 目前仅支持 Docker Hub 公开镜像
 */
router.post('/docker/check-update', async (req, res) => {
  try {
    const { serverId, containerId } = req.body;

    if (!serverId) {
      return res.status(400).json({ success: false, error: '缺少服务器 ID' });
    }

    // 检查主机是否在线
    if (!agentService.isOnline(serverId)) {
      return res.status(400).json({ success: false, error: '主机不在线' });
    }

    // 构建任务数据
    const taskData = JSON.stringify({
      container_id: containerId || '',
    });

    // 发送任务并等待结果 (检查更新可能需要网络请求，给 3 分钟超时)
    const result = await agentService.sendTaskAndWait(
      serverId,
      {
        type: DockerTaskTypes.DOCKER_CHECK_UPDATE,
        data: taskData,
        timeout: 180,
      },
      180000
    );

    if (result.successful) {
      // 解析 Agent 返回的 JSON 数据
      let updateStatus = [];
      try {
        updateStatus = JSON.parse(result.data);
      } catch (e) {
        updateStatus = result.data;
      }

      res.json({
        success: true,
        message: '检查完成',
        data: updateStatus,
      });
    } else {
      res.status(400).json({
        success: false,
        error: result.data || '检查更新失败',
      });
    }
  } catch (error) {
    res.status(500).json({ success: false, error: error.message });
  }
});

// ==================== Docker 镜像管理 ====================

/**
 * 获取 Docker 镜像列表
 * POST /docker/images
 */
router.post('/docker/images', async (req, res) => {
  try {
    const { serverId } = req.body;
    if (!serverId) return res.status(400).json({ success: false, error: '缺少服务器 ID' });
    if (!agentService.isOnline(serverId)) return res.status(400).json({ success: false, error: '主机不在线' });

    const result = await agentService.sendTaskAndWait(serverId, {
      type: DockerTaskTypes.DOCKER_IMAGES,
      data: '',
      timeout: 30,
    }, 30000);

    if (result.successful) {
      res.json({ success: true, data: JSON.parse(result.data) });
    } else {
      res.status(400).json({ success: false, error: result.data });
    }
  } catch (error) {
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * Docker 镜像操作 (pull/remove/prune)
 * POST /docker/image/action
 */
router.post('/docker/image/action', async (req, res) => {
  try {
    const { serverId, action, image } = req.body;
    if (!serverId || !action) return res.status(400).json({ success: false, error: '缺少参数' });
    if (!agentService.isOnline(serverId)) return res.status(400).json({ success: false, error: '主机不在线' });

    const taskData = JSON.stringify({ action, image });
    const timeout = action === 'pull' ? 300 : 60; // 拉取镜像可能需要较长时间

    const result = await agentService.sendTaskAndWait(serverId, {
      type: DockerTaskTypes.DOCKER_IMAGE_ACTION,
      data: taskData,
      timeout,
    }, timeout * 1000);

    if (result.successful) {
      res.json({ success: true, message: result.data });
    } else {
      res.status(400).json({ success: false, error: result.data });
    }
  } catch (error) {
    res.status(500).json({ success: false, error: error.message });
  }
});

// ==================== Docker 网络管理 ====================

/**
 * 获取 Docker 网络列表
 * POST /docker/networks
 */
router.post('/docker/networks', async (req, res) => {
  try {
    const { serverId } = req.body;
    if (!serverId) return res.status(400).json({ success: false, error: '缺少服务器 ID' });
    if (!agentService.isOnline(serverId)) return res.status(400).json({ success: false, error: '主机不在线' });

    const result = await agentService.sendTaskAndWait(serverId, {
      type: DockerTaskTypes.DOCKER_NETWORKS,
      data: '',
      timeout: 30,
    }, 30000);

    if (result.successful) {
      res.json({ success: true, data: JSON.parse(result.data) });
    } else {
      res.status(400).json({ success: false, error: result.data });
    }
  } catch (error) {
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * Docker 网络操作 (create/remove/connect/disconnect)
 * POST /docker/network/action
 */
router.post('/docker/network/action', async (req, res) => {
  try {
    const { serverId, action, name, driver, subnet, gateway, container } = req.body;
    if (!serverId || !action) return res.status(400).json({ success: false, error: '缺少参数' });
    if (!agentService.isOnline(serverId)) return res.status(400).json({ success: false, error: '主机不在线' });

    const taskData = JSON.stringify({ action, name, driver, subnet, gateway, container });

    const result = await agentService.sendTaskAndWait(serverId, {
      type: DockerTaskTypes.DOCKER_NETWORK_ACTION,
      data: taskData,
      timeout: 30,
    }, 30000);

    if (result.successful) {
      res.json({ success: true, message: result.data });
    } else {
      res.status(400).json({ success: false, error: result.data });
    }
  } catch (error) {
    res.status(500).json({ success: false, error: error.message });
  }
});

// ==================== Docker Volume 管理 ====================

/**
 * 获取 Docker Volume 列表
 * POST /docker/volumes
 */
router.post('/docker/volumes', async (req, res) => {
  try {
    const { serverId } = req.body;
    if (!serverId) return res.status(400).json({ success: false, error: '缺少服务器 ID' });
    if (!agentService.isOnline(serverId)) return res.status(400).json({ success: false, error: '主机不在线' });

    const result = await agentService.sendTaskAndWait(serverId, {
      type: DockerTaskTypes.DOCKER_VOLUMES,
      data: '',
      timeout: 30,
    }, 30000);

    if (result.successful) {
      res.json({ success: true, data: JSON.parse(result.data) });
    } else {
      res.status(400).json({ success: false, error: result.data });
    }
  } catch (error) {
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * Docker Volume 操作 (create/remove/prune)
 * POST /docker/volume/action
 */
router.post('/docker/volume/action', async (req, res) => {
  try {
    const { serverId, action, name, driver } = req.body;
    if (!serverId || !action) return res.status(400).json({ success: false, error: '缺少参数' });
    if (!agentService.isOnline(serverId)) return res.status(400).json({ success: false, error: '主机不在线' });

    const taskData = JSON.stringify({ action, name, driver });

    const result = await agentService.sendTaskAndWait(serverId, {
      type: DockerTaskTypes.DOCKER_VOLUME_ACTION,
      data: taskData,
      timeout: 30,
    }, 30000);

    if (result.successful) {
      res.json({ success: true, message: result.data });
    } else {
      res.status(400).json({ success: false, error: result.data });
    }
  } catch (error) {
    res.status(500).json({ success: false, error: error.message });
  }
});

// ==================== Docker 日志 ====================

/**
 * 获取容器日志
 * POST /docker/logs
 */
router.post('/docker/logs', async (req, res) => {
  try {
    const { serverId, containerId, tail, since } = req.body;
    if (!serverId || !containerId) return res.status(400).json({ success: false, error: '缺少参数' });
    if (!agentService.isOnline(serverId)) return res.status(400).json({ success: false, error: '主机不在线' });

    const taskData = JSON.stringify({ container_id: containerId, tail: tail || 100, since });

    const result = await agentService.sendTaskAndWait(serverId, {
      type: DockerTaskTypes.DOCKER_LOGS,
      data: taskData,
      timeout: 30,
    }, 30000);

    if (result.successful) {
      res.json({ success: true, data: result.data });
    } else {
      res.status(400).json({ success: false, error: result.data });
    }
  } catch (error) {
    res.status(500).json({ success: false, error: error.message });
  }
});

// ==================== Docker 资源统计 ====================

/**
 * 获取容器资源统计
 * POST /docker/stats
 */
router.post('/docker/stats', async (req, res) => {
  try {
    const { serverId } = req.body;
    if (!serverId) return res.status(400).json({ success: false, error: '缺少服务器 ID' });
    if (!agentService.isOnline(serverId)) return res.status(400).json({ success: false, error: '主机不在线' });

    const result = await agentService.sendTaskAndWait(serverId, {
      type: DockerTaskTypes.DOCKER_STATS,
      data: '',
      timeout: 30,
    }, 30000);

    if (result.successful) {
      res.json({ success: true, data: JSON.parse(result.data) });
    } else {
      res.status(400).json({ success: false, error: result.data });
    }
  } catch (error) {
    res.status(500).json({ success: false, error: error.message });
  }
});

// ==================== 终端接口 ====================

/**
 * 执行 SSH 命令（非交互式）
 */
router.post('/ssh/exec', async (req, res) => {
  try {
    const { serverId, command } = req.body;
    if (!serverId || !command) return res.status(400).json({ success: false, error: '缺少参数' });

    const server = serverStorage.getById(serverId);
    if (!server) return res.status(404).json({ success: false, error: '服务器不存在' });

    const result = await sshService.executeCommand(serverId, server, command);
    res.json(result);
  } catch (error) {
    res.status(500).json({ success: false, error: error.message });
  }
});

router.post('/ssh/disconnect', (req, res) => {
  const { serverId } = req.body;
  if (serverId) {
    sshService.closeConnection(serverId);
  }
  res.json({ success: true, message: 'SSH 会话已请求断开' });
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
    // 同时也重启历史指标采集，因为可能修改了采集间隔
    if (req.body.metrics_collect_interval !== undefined) {
      agentService.startHistoryCollector();
    }
    res.json({ success: true, message: '监控配置更新成功', data: config });
  } catch (error) {
    res.status(500).json({ success: false, error: error.message });
  }
});

// ==================== 历史指标接口 ====================

/**
 * 获取历史指标记录
 */
router.get('/metrics/history', (req, res) => {
  try {
    const { serverId, startTime, endTime, page = 1, pageSize = 50 } = req.query;

    const limit = Math.min(parseInt(pageSize) || 50, 10000);
    const offset = ((parseInt(page) || 1) - 1) * limit;

    const records = ServerMetricsHistory.getHistory({
      serverId: serverId || null,
      startTime: startTime || null,
      endTime: endTime || null,
      limit,
      offset,
    });

    const total = ServerMetricsHistory.getCount({
      serverId: serverId || null,
      startTime: startTime || null,
      endTime: endTime || null,
    });

    res.json({
      success: true,
      data: records,
      pagination: {
        page: parseInt(page) || 1,
        pageSize: limit,
        total,
        totalPages: Math.ceil(total / limit),
      },
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
    const status = monitorService.getStatus();
    res.json({ success: true, data: status });
  } catch (error) {
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * 手动触发一次指标采集并存入历史记录
 */
router.post('/metrics/collect', async (req, res) => {
  try {
    const servers = serverStorage.getAll();
    const collected = [];

    for (const server of servers) {
      const metrics = agentService.getMetrics(server.id);
      if (metrics) {
        // 保存到历史记录
        ServerMetricsHistory.create({
          server_id: server.id,
          cpu_usage: parseFloat(metrics.cpu_usage) || 0,
          cpu_load: metrics.load || '',
          cpu_cores: metrics.cores || 1,
          mem_used: metrics.mem_used || 0,
          mem_total: metrics.mem_total || 0,
          mem_usage: metrics.mem_percent || 0,
          disk_used: metrics.disk_used || '',
          disk_total: metrics.disk_total || '',
          disk_usage: metrics.disk_percent || 0,
          docker_installed: metrics.docker?.installed ? 1 : 0,
          docker_running: metrics.docker?.running || 0,
          docker_stopped: metrics.docker?.stopped || 0,
          gpu_usage: parseFloat(metrics.gpu_usage) || 0,
          gpu_mem_used: metrics.gpu_mem_used || 0,
          gpu_mem_total: metrics.gpu_mem_total || 0,
          gpu_power: parseFloat(metrics.gpu_power) || 0,
          platform: metrics.platform || '',
        });
        collected.push(server.id);
      }
    }

    res.json({
      success: true,
      message: `已采集 ${collected.length} 台在线主机的实时指标`,
      collected: collected.length,
    });
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

    // 持久化到数据库
    const config = ServerMonitorConfig.get();
    if (config) {
      ServerMonitorConfig.update({
        ...config,
        metrics_collect_interval: Math.floor(interval / 1000), // 转为秒存储
      });
      // 关键修复：更新配置后必须立即重启采集定时器，否则新间隔不会生效
      agentService.startHistoryCollector();
    }

    res.json({ success: true, message: '采集间隔已更新并保存' });
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

/**
 * 清空历史指标记录
 */
router.delete('/metrics/history/clear', (req, res) => {
  try {
    const { serverId = null } = req.query;
    const deleted = ServerMetricsHistory.clear(serverId);
    res.json({
      success: true,
      message: serverId
        ? `已清空指定主机的 ${deleted} 条记录`
        : `已清空所有主机的 ${deleted} 条指标记录`,
    });
  } catch (error) {
    res.status(500).json({ success: false, error: error.message });
  }
});

// ==================== 任务下发接口 ====================

const { TaskTypes } = require('./protocol');

/**
 * 向指定主机执行命令
 */
router.post('/task/command/:serverId', async (req, res) => {
  try {
    const { serverId } = req.params;
    const { command, timeout = 60000 } = req.body;

    if (!command) {
      return res.status(400).json({ success: false, error: '缺少命令' });
    }

    // 检查主机是否在线
    if (!agentService.isOnline(serverId)) {
      return res.status(400).json({ success: false, error: '主机不在线' });
    }

    const taskId = require('crypto').randomUUID();
    const result = agentService.sendTask(serverId, {
      id: taskId,
      type: TaskTypes.COMMAND,
      data: command,
      timeout,
    });

    if (!result) {
      return res.status(500).json({ success: false, error: '任务下发失败' });
    }

    res.json({
      success: true,
      data: {
        taskId,
        serverId,
        command,
        status: 'sent',
      },
    });
  } catch (error) {
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * 向指定主机执行命令并等待结果
 */
router.post('/task/command/:serverId/sync', async (req, res) => {
  try {
    const { serverId } = req.params;
    const { command, timeout = 60000 } = req.body;

    if (!command) {
      return res.status(400).json({ success: false, error: '缺少命令' });
    }

    // 检查主机是否在线
    if (!agentService.isOnline(serverId)) {
      return res.status(400).json({ success: false, error: '主机不在线' });
    }

    // 使用 Promise 等待任务结果
    const result = await agentService.sendTaskAndWait(
      serverId,
      {
        type: TaskTypes.COMMAND,
        data: command,
        timeout,
      },
      timeout + 5000
    );

    res.json({
      success: result.successful,
      data: {
        output: result.data,
        delay: result.delay,
        successful: result.successful,
      },
    });
  } catch (error) {
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * 批量向多个主机执行命令
 */
router.post('/task/command/batch', async (req, res) => {
  try {
    const { serverIds, command, timeout = 60000 } = req.body;

    if (!Array.isArray(serverIds) || serverIds.length === 0) {
      return res.status(400).json({ success: false, error: '缺少目标主机列表' });
    }
    if (!command) {
      return res.status(400).json({ success: false, error: '缺少命令' });
    }

    const results = [];
    for (const serverId of serverIds) {
      const online = agentService.isOnline(serverId);
      if (!online) {
        results.push({ serverId, success: false, error: '主机不在线' });
        continue;
      }

      const taskId = require('crypto').randomUUID();
      const sent = agentService.sendTask(serverId, {
        id: taskId,
        type: TaskTypes.COMMAND,
        data: command,
        timeout,
      });

      results.push({
        serverId,
        success: sent,
        taskId: sent ? taskId : null,
      });
    }

    res.json({
      success: true,
      data: {
        total: serverIds.length,
        sent: results.filter(r => r.success).length,
        results,
      },
    });
  } catch (error) {
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * 获取主机连接状态
 */
router.get('/task/status/:serverId', (req, res) => {
  try {
    const { serverId } = req.params;
    const online = agentService.isOnline(serverId);
    const hostInfo = agentService.getHostInfo ? agentService.getHostInfo(serverId) : null;

    res.json({
      success: true,
      data: {
        serverId,
        online,
        hostInfo,
        canExecuteTask: online,
      },
    });
  } catch (error) {
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * 请求主机上报系统信息
 */
router.post('/task/refresh/:serverId', (req, res) => {
  try {
    const { serverId } = req.params;

    if (!agentService.isOnline(serverId)) {
      return res.status(400).json({ success: false, error: '主机不在线' });
    }

    const result = agentService.requestHostInfo(serverId);

    res.json({
      success: result,
      message: result ? '已请求主机上报信息' : '请求失败',
    });
  } catch (error) {
    res.status(500).json({ success: false, error: error.message });
  }
});

module.exports = router;
