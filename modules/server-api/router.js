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
 * 服务器电源操作 (重启/关机)
 * POST /action
 * { serverId, action: 'reboot'|'shutdown' }
 */
router.post('/action', async (req, res) => {
  try {
    const { serverId, action } = req.body;
    if (!serverId || !action) return res.status(400).json({ success: false, error: '缺少参数' });

    const server = serverStorage.getById(serverId);
    if (!server) return res.status(404).json({ success: false, error: '服务器不存在' });

    // 智能识别操作系统
    let platform = 'linux';
    const hostInfo = agentService.getHostInfo ? agentService.getHostInfo(serverId) : null;
    const metrics = agentService.getMetrics ? agentService.getMetrics(serverId) : null;

    if (hostInfo && hostInfo.platform) {
      platform = hostInfo.platform.toLowerCase();
    } else if (metrics && metrics.platform) {
      platform = metrics.platform.toLowerCase();
    } else if (server.os) {
      platform = server.os.toLowerCase();
    }

    const isWindows = platform.includes('win');
    let command = '';

    if (action === 'reboot') {
      command = isWindows ? 'shutdown /r /t 0 /f' : 'sudo reboot';
    } else if (action === 'shutdown') {
      command = isWindows ? 'shutdown /s /t 0 /f' : 'sudo shutdown -h now';
    } else {
      return res.status(400).json({ success: false, error: '不支持的操作类型' });
    }

    // 优先使用 Agent 执行
    if (agentService.isOnline(serverId)) {
      const { TaskTypes } = require('./protocol');
      const taskId = require('crypto').randomUUID();
      agentService.sendTask(serverId, {
        id: taskId,
        type: TaskTypes.COMMAND,
        data: command,
        timeout: 10,
      });
      return res.json({ success: true, message: '电源管理命令已发送给 Agent' });
    }

    // 回退到 SSH 执行
    const result = await sshService.executeCommand(serverId, server, command);
    if (result.success) {
      res.json({ success: true, message: 'SSH 命令执行成功' });
    } else {
      res.status(500).json({ success: false, message: 'SSH 执行失败: ' + (result.error || result.stderr) });
    }
  } catch (error) {
    res.status(500).json({ success: false, error: error.message });
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
 * 优先使用 Agent，如无 Agent 则通过 SSH 获取基础状态
 */
router.post('/info', async (req, res) => {
  try {
    const { serverId, force } = req.body;
    if (!serverId) return res.status(400).json({ success: false, error: '缺少服务器 ID' });

    const server = serverStorage.getById(serverId);
    if (!server) return res.status(404).json({ success: false, error: '服务器不存在' });

    // 尝试获取 Agent 指标
    const metrics = agentService.getMetrics(serverId);
    if (metrics) {
      return res.json({
        success: true,
        ...metrics,
        is_agent: true,
      });
    }

    // 无 Agent 数据，尝试通过 SSH 获取基础状态
    const sshService = require('./ssh-service');

    // 增强版命令，包含 1秒网速采样
    const infoCommand = `
      IFACE=$(ip route get 8.8.8.8 2>/dev/null | grep dev | awk '{print $5}' || echo "eth0")
      read r1 t1 < <(cat /proc/net/dev | grep "$IFACE" | awk '{print $2, $10}' || echo "0 0")
      sleep 1
      read r2 t2 < <(cat /proc/net/dev | grep "$IFACE" | awk '{print $2, $10}' || echo "0 0")
      
      echo "===SYSTEM==="
      uname -s 2>/dev/null || echo "Unknown"
      echo "===UPTIME==="
      cat /proc/uptime 2>/dev/null | cut -d' ' -f1 || echo "0"
      echo "===CPU==="
      grep -c ^processor /proc/cpuinfo 2>/dev/null || echo "1"
      echo "===LOAD==="
      cat /proc/loadavg 2>/dev/null | cut -d' ' -f1-3 || echo "0 0 0"
      echo "===CPU_USAGE==="
      top -bn1 2>/dev/null | grep "Cpu(s)" | awk '{print $2}' | cut -d'%' -f1 | head -1 || echo "0"
      echo "===MEMORY==="
      free -b 2>/dev/null | grep Mem | awk '{printf "%.0f %.0f", $3, $2}' || echo "0 0"
      echo "===DISK==="
      df -B1 / 2>/dev/null | tail -1 | awk '{printf "%.0f %.0f %s", $3, $2, $5}' || echo "0 0 0%"
      echo "===NET==="
      echo "$(( r2-r1 )) $(( t2-t1 ))"
    `.trim();

    const result = await sshService.executeCommand(serverId, server, infoCommand, 15000);

    if (!result.success) {
      return res.status(500).json({
        success: false,
        error: 'SSH 连接失败: ' + (result.error || result.stderr || '未知错误'),
        is_agent: false,
      });
    }

    const output = result.stdout || '';
    const parseSection = (name) => {
      const regex = new RegExp(`===\\s*${name}\\s*===\\s*([\\s\\S]*?)(?====|$)`, 'i');
      const match = output.match(regex);
      return match ? match[1].trim() : '';
    };

    const platform = parseSection('SYSTEM') || 'Linux';
    const uptimeSeconds = parseFloat(parseSection('UPTIME')) || 0;
    const cores = parseInt(parseSection('CPU')) || 1;
    const load = parseSection('LOAD') || '0 0 0';
    const cpuRate = parseFloat(parseSection('CPU_USAGE')) || 0;

    const memStr = parseSection('MEMORY').split(/\s+/);
    const mUsed = parseInt(memStr[0]) || 0;
    const mTotal = parseInt(memStr[1]) || 1;

    const diskStr = parseSection('DISK').split(/\s+/);
    const dUsed = parseInt(diskStr[0]) || 0;
    const dTotal = parseInt(diskStr[1]) || 1;
    const dPerc = diskStr[2] || '0%';

    const netStr = parseSection('NET').split(/\s+/);
    const rb = parseInt(netStr[0]) || 0;
    const tb = parseInt(netStr[1]) || 0;

    const fmt = (b) => {
      if (b <= 0) return '0 B';
      const k = 1024;
      const ss = ['B', 'KB', 'MB', 'GB', 'TB'];
      const i = Math.floor(Math.log(b) / Math.log(k));
      return parseFloat((b / Math.pow(k, i)).toFixed(1)) + ' ' + ss[i];
    };

    res.json({
      success: true,
      platform,
      uptime: Math.floor(uptimeSeconds),
      cores,
      load,
      cpu_usage: cpuRate.toFixed(1) + '%',
      mem: `${fmt(mUsed)} / ${fmt(mTotal)}`,
      mem_percent: Math.round((mUsed / mTotal) * 100),
      disk: `${fmt(dUsed)} / ${fmt(dTotal)} (${dPerc})`,
      network: {
        down: fmt(rb) + '/s',
        up: fmt(tb) + '/s',
        rx_speed: fmt(rb) + '/s',
        tx_speed: fmt(tb) + '/s'
      },
      is_agent: false,
      source: 'ssh',
    });
  } catch (error) {
    res.status(500).json({ success: false, error: error.message });
  }
});

// ==================== V2 任务与 Docker 聚合 API ====================

function parseJsonSafe(value, fallback = []) {
  if (value === null || value === undefined || value === '') return fallback;
  if (Array.isArray(value) || typeof value === 'object') return value;
  try {
    return JSON.parse(value);
  } catch (error) {
    return fallback;
  }
}

function toArraySafe(value) {
  if (Array.isArray(value)) return value;
  if (!value || typeof value !== 'object') return [];

  const candidateKeys = ['data', 'items', 'list', 'results', 'projects', 'containers', 'rows'];
  for (const key of candidateKeys) {
    if (Array.isArray(value[key])) {
      return value[key];
    }
  }

  // 兜底：若对象中存在唯一一个数组字段，则直接取该字段
  const arrayValues = Object.values(value).filter(item => Array.isArray(item));
  if (arrayValues.length === 1) {
    return arrayValues[0];
  }

  return [];
}

function toTaskData(data) {
  if (data === '' || data === null || data === undefined) return '';
  if (typeof data === 'string') return data;
  return JSON.stringify(data);
}

function buildDockerV2Task(action, payload = {}) {
  const defaultTimeoutMs = 60000;

  switch (action) {
    case 'container.start':
    case 'container.stop':
    case 'container.restart':
    case 'container.pause':
    case 'container.unpause':
    case 'container.pull':
      if (!payload.containerId) throw new Error('缺少 containerId');
      return {
        type: DockerTaskTypes.DOCKER_ACTION,
        data: {
          action: action.split('.')[1],
          container_id: payload.containerId,
          image: payload.image || '',
        },
        timeoutMs: 120000,
      };
    case 'container.update':
      if (!payload.containerId || !payload.containerName) {
        throw new Error('缺少 containerId 或 containerName');
      }
      return {
        type: DockerTaskTypes.DOCKER_UPDATE_CONTAINER,
        data: {
          container_id: payload.containerId,
          container_name: payload.containerName,
          image: payload.image || '',
        },
        agentTimeoutSec: 600,
        timeoutMs: 10 * 60 * 1000,
        trackProgress: true,
      };
    case 'container.rename':
      if (!payload.containerId || !payload.newName) {
        throw new Error('缺少 containerId 或 newName');
      }
      return {
        type: DockerTaskTypes.DOCKER_RENAME_CONTAINER,
        data: {
          container_id: payload.containerId,
          new_name: payload.newName,
        },
        timeoutMs: defaultTimeoutMs,
      };
    case 'container.logs':
      if (!payload.containerId) throw new Error('缺少 containerId');
      return {
        type: DockerTaskTypes.DOCKER_LOGS,
        data: {
          container_id: payload.containerId,
          tail: payload.tail || 100,
          since: payload.since || '',
        },
        timeoutMs: defaultTimeoutMs,
      };
    case 'container.checkUpdates':
      return {
        type: DockerTaskTypes.DOCKER_CHECK_UPDATE,
        data: {
          container_id: payload.containerId || '',
        },
        timeoutMs: 180000,
      };
    case 'container.create':
      if (!payload.image) throw new Error('缺少镜像名称 image');
      return {
        type: DockerTaskTypes.DOCKER_CREATE_CONTAINER,
        data: {
          name: payload.name || '',
          image: payload.image,
          ports: Array.isArray(payload.ports) ? payload.ports : [],
          volumes: Array.isArray(payload.volumes) ? payload.volumes : [],
          env: payload.env && typeof payload.env === 'object' ? payload.env : {},
          network: payload.network || '',
          restart: payload.restart || 'unless-stopped',
          privileged: !!payload.privileged,
          extra_args: Array.isArray(payload.extraArgs) ? payload.extraArgs : [],
        },
        agentTimeoutSec: 300,
        timeoutMs: 300000,
      };
    case 'image.list':
      return {
        type: DockerTaskTypes.DOCKER_IMAGES,
        data: '',
        timeoutMs: defaultTimeoutMs,
      };
    case 'image.pull':
    case 'image.remove':
    case 'image.prune':
      return {
        type: DockerTaskTypes.DOCKER_IMAGE_ACTION,
        data: {
          action: action.split('.')[1],
          image: payload.image || '',
        },
        agentTimeoutSec: action === 'image.pull' ? 300 : 60,
        timeoutMs: action === 'image.pull' ? 300000 : defaultTimeoutMs,
      };
    case 'network.list':
      return {
        type: DockerTaskTypes.DOCKER_NETWORKS,
        data: '',
        timeoutMs: defaultTimeoutMs,
      };
    case 'network.create':
    case 'network.remove':
    case 'network.connect':
    case 'network.disconnect':
      return {
        type: DockerTaskTypes.DOCKER_NETWORK_ACTION,
        data: {
          action: action.split('.')[1],
          name: payload.name || '',
          driver: payload.driver || '',
          subnet: payload.subnet || '',
          gateway: payload.gateway || '',
          container: payload.container || '',
        },
        timeoutMs: defaultTimeoutMs,
      };
    case 'volume.list':
      return {
        type: DockerTaskTypes.DOCKER_VOLUMES,
        data: '',
        timeoutMs: defaultTimeoutMs,
      };
    case 'volume.create':
    case 'volume.remove':
    case 'volume.prune':
      return {
        type: DockerTaskTypes.DOCKER_VOLUME_ACTION,
        data: {
          action: action.split('.')[1],
          name: payload.name || '',
          driver: payload.driver || '',
        },
        timeoutMs: defaultTimeoutMs,
      };
    case 'stats.list':
      return {
        type: DockerTaskTypes.DOCKER_STATS,
        data: '',
        timeoutMs: defaultTimeoutMs,
      };
    case 'compose.list':
      return {
        type: DockerTaskTypes.DOCKER_COMPOSE_LIST,
        data: '',
        timeoutMs: defaultTimeoutMs,
      };
    case 'compose.up':
    case 'compose.down':
    case 'compose.restart':
    case 'compose.pull':
      if (!payload.project) throw new Error('缺少 project');
      return {
        type: DockerTaskTypes.DOCKER_COMPOSE_ACTION,
        data: {
          action: action.split('.')[1],
          project: payload.project,
          config_dir: payload.configDir || '',
        },
        agentTimeoutSec: action === 'compose.pull' ? 300 : 120,
        timeoutMs: action === 'compose.pull' ? 300000 : 120000,
      };
    default:
      throw new Error(`不支持的 Docker action: ${action}`);
  }
}

async function loadDockerOverviewForServer(server) {
  const serverId = server?.id || '';
  const serverName = server?.name || '未知主机';
  const host = server?.host || '';

  const emptyOverview = errorMessage => ({
    serverId,
    serverName,
    host,
    online: !!(serverId && agentService.isOnline(serverId)),
    docker: {
      installed: false,
      running: 0,
      stopped: 0,
      containers: [],
    },
    resources: {
      images: [],
      networks: [],
      volumes: [],
      stats: [],
      composeProjects: [],
    },
    errors: {
      overview: errorMessage || '',
      images: errorMessage || '',
      networks: errorMessage || '',
      volumes: errorMessage || '',
      stats: errorMessage || '',
      composeProjects: errorMessage || '',
    },
  });

  try {
    if (!serverId) {
      throw new Error('主机配置无效: 缺少 serverId');
    }

    const metrics = agentService.getMetrics(serverId) || {};
    const docker = metrics.docker || {};

    const runAgentTask = async (type, timeoutMs = 30000) => {
      try {
        const result = await agentService.sendTaskAndWait(
          serverId,
          {
            type,
            data: '',
            timeout: Math.ceil(timeoutMs / 1000),
          },
          timeoutMs
        );
        if (!result || typeof result !== 'object') {
          return { ok: false, error: '任务返回格式无效', data: [] };
        }
        if (!result.successful) {
          return { ok: false, error: result.data || '任务执行失败', data: [] };
        }
        const parsed = parseJsonSafe(result.data, []);
        return { ok: true, data: toArraySafe(parsed) };
      } catch (error) {
        return { ok: false, error: error.message, data: [] };
      }
    };

    const [imagesRes, networksRes, volumesRes, statsRes, composeRes] = await Promise.all([
      runAgentTask(DockerTaskTypes.DOCKER_IMAGES),
      runAgentTask(DockerTaskTypes.DOCKER_NETWORKS),
      runAgentTask(DockerTaskTypes.DOCKER_VOLUMES),
      runAgentTask(DockerTaskTypes.DOCKER_STATS),
      runAgentTask(DockerTaskTypes.DOCKER_COMPOSE_LIST),
    ]);

    return {
      serverId,
      serverName,
      host,
      online: true,
      docker: {
        installed: !!docker.installed,
        running: docker.running || 0,
        stopped: docker.stopped || 0,
        containers: Array.isArray(docker.containers) ? docker.containers : [],
      },
      resources: {
        images: toArraySafe(imagesRes.data),
        networks: toArraySafe(networksRes.data),
        volumes: toArraySafe(volumesRes.data),
        stats: toArraySafe(statsRes.data),
        composeProjects: toArraySafe(composeRes.data),
      },
      errors: {
        overview: '',
        images: imagesRes.ok ? '' : imagesRes.error,
        networks: networksRes.ok ? '' : networksRes.error,
        volumes: volumesRes.ok ? '' : volumesRes.error,
        stats: statsRes.ok ? '' : statsRes.error,
        composeProjects: composeRes.ok ? '' : composeRes.error,
      },
    };
  } catch (error) {
    return emptyOverview(error.message || 'Docker 概览加载失败');
  }
}

router.post('/v2/tasks', async (req, res) => {
  try {
    const { serverId, domain, action, payload, requestId } = req.body || {};

    if (!serverId || !domain || !action) {
      return res.status(400).json({ success: false, error: '缺少 serverId/domain/action' });
    }
    if (domain !== 'docker') {
      return res.status(400).json({ success: false, error: `不支持的 domain: ${domain}` });
    }
    if (!agentService.isOnline(serverId)) {
      return res.status(400).json({ success: false, error: '主机不在线' });
    }

    if (requestId) {
      const existing = agentService.getTask(requestId);
      if (existing) {
        return res.status(202).json({
          success: true,
          data: {
            taskId: existing.taskId,
            serverId,
            domain,
            action,
            acceptedAt: existing.createdAt,
            deduped: true,
          },
        });
      }
    }

    const mapped = buildDockerV2Task(action, payload || {});
    const taskId = agentService.submitTask(
      serverId,
      {
        id: requestId || undefined,
        type: mapped.type,
        data: toTaskData(mapped.data),
        timeout: mapped.agentTimeoutSec || Math.ceil((mapped.timeoutMs || 60000) / 1000),
      },
      {
        waitForResult: false,
        timeoutMs: mapped.timeoutMs || 60000,
        trackProgress: !!mapped.trackProgress,
        domain: 'docker',
        action,
      }
    );

    res.status(202).json({
      success: true,
      data: {
        taskId,
        serverId,
        domain,
        action,
        acceptedAt: Date.now(),
      },
    });
  } catch (error) {
    res.status(400).json({ success: false, error: error.message });
  }
});

router.get('/v2/tasks/stream', (req, res) => {
  const serverId = req.query.serverId ? String(req.query.serverId) : '';
  const bootstrapLimit = Math.max(1, Math.min(200, parseInt(req.query.bootstrapLimit) || 50));

  res.setHeader('Content-Type', 'text/event-stream; charset=utf-8');
  res.setHeader('Cache-Control', 'no-cache, no-transform');
  res.setHeader('Connection', 'keep-alive');
  res.setHeader('X-Accel-Buffering', 'no');
  if (typeof res.flushHeaders === 'function') {
    res.flushHeaders();
  }

  const writeEvent = (event, data) => {
    res.write(`event: ${event}\n`);
    res.write(`data: ${JSON.stringify(data)}\n\n`);
  };

  writeEvent('ready', {
    connected: true,
    timestamp: Date.now(),
    serverId: serverId || null,
  });

  const recentTasks = agentService.getRecentTasks(serverId, bootstrapLimit);
  for (const task of recentTasks) {
    writeEvent('task.update', task);
  }

  const onTaskUpdate = task => {
    if (serverId && task.serverId !== serverId) return;
    writeEvent('task.update', task);
  };
  agentService.on('task:update', onTaskUpdate);

  const heartbeat = setInterval(() => {
    writeEvent('ping', { timestamp: Date.now() });
  }, 15000);
  if (typeof heartbeat.unref === 'function') {
    heartbeat.unref();
  }

  req.on('close', () => {
    clearInterval(heartbeat);
    agentService.off('task:update', onTaskUpdate);
    res.end();
  });
});

router.get('/v2/tasks', (req, res) => {
  const serverId = req.query.serverId ? String(req.query.serverId) : '';
  const limit = Math.max(1, Math.min(500, parseInt(req.query.limit) || 100));
  res.json({
    success: true,
    data: agentService.getRecentTasks(serverId, limit),
  });
});

router.get('/v2/tasks/:taskId', (req, res) => {
  const task = agentService.getTask(req.params.taskId);
  if (!task) {
    return res.status(404).json({ success: false, error: '任务不存在' });
  }
  res.json({ success: true, data: task });
});

router.get('/v2/docker/overview', async (req, res) => {
  try {
    const selectedServerId = req.query.serverId ? String(req.query.serverId) : '';
    const serversRaw = serverStorage.getAll();
    const servers = Array.isArray(serversRaw) ? serversRaw.filter(item => item && item.id) : [];

    let targetServers = [];
    if (selectedServerId) {
      const server = servers.find(item => item.id === selectedServerId);
      if (!server) {
        return res.status(404).json({ success: false, error: '主机不存在' });
      }
      if (!agentService.isOnline(server.id)) {
        return res.status(400).json({ success: false, error: '主机不在线' });
      }
      targetServers = [server];
    } else {
      targetServers = servers.filter(item => agentService.isOnline(item.id));
    }

    const overviewResults = await Promise.allSettled(
      targetServers.map(server => loadDockerOverviewForServer(server))
    );

    const overviews = overviewResults.map((entry, index) => {
      if (entry.status === 'fulfilled') {
        return entry.value;
      }

      const fallbackServer = targetServers[index];
      return {
        serverId: fallbackServer?.id || '',
        serverName: fallbackServer?.name || '未知主机',
        host: fallbackServer?.host || '',
        online: false,
        docker: {
          installed: false,
          running: 0,
          stopped: 0,
          containers: [],
        },
        resources: {
          images: [],
          networks: [],
          volumes: [],
          stats: [],
          composeProjects: [],
        },
        errors: {
          overview: entry.reason?.message || '主机 Docker 概览加载失败',
          images: '',
          networks: '',
          volumes: '',
          stats: '',
          composeProjects: '',
        },
      };
    });

    const summary = overviews.reduce(
      (acc, item) => {
        const containers = Array.isArray(item?.docker?.containers) ? item.docker.containers : [];
        const images = toArraySafe(item?.resources?.images);
        const networks = toArraySafe(item?.resources?.networks);
        const volumes = toArraySafe(item?.resources?.volumes);
        const composeProjects = toArraySafe(item?.resources?.composeProjects);

        acc.hosts += 1;
        acc.containers += containers.length;
        acc.running += item?.docker?.running || 0;
        acc.stopped += item?.docker?.stopped || 0;
        acc.images += images.length;
        acc.networks += networks.length;
        acc.volumes += volumes.length;
        acc.composeProjects += composeProjects.length;
        return acc;
      },
      {
        hosts: 0,
        containers: 0,
        running: 0,
        stopped: 0,
        images: 0,
        networks: 0,
        volumes: 0,
        composeProjects: 0,
      }
    );

    res.json({
      success: true,
      data: {
        generatedAt: Date.now(),
        servers: overviews,
        summary,
      },
    });
  } catch (error) {
    console.error('[ServerAPI] /v2/docker/overview failed:', error);
    res.json({
      success: false,
      error: error.message || 'Docker 概览加载失败',
      data: {
        generatedAt: Date.now(),
        servers: [],
        summary: {
          hosts: 0,
          containers: 0,
          running: 0,
          stopped: 0,
          images: 0,
          networks: 0,
          volumes: 0,
          composeProjects: 0,
        },
      },
    });
  }
});

/**
 * Docker 容器操作
 * POST /docker/action
 * { serverId, containerId, action: 'start'|'stop'|'restart'|'pause'|'unpause'|'update'|'pull', image?: string }
 */

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

/**
 * Docker 容器一键更新
 * POST /docker/container/update
 * { serverId, containerId, containerName, image? }
 */
router.post('/docker/container/update', async (req, res) => {
  try {
    const { serverId, containerId, containerName, image } = req.body;

    if (!serverId || !containerId || !containerName) {
      return res.status(400).json({ success: false, error: '缺少必要参数' });
    }

    if (!agentService.isOnline(serverId)) {
      return res.status(400).json({ success: false, error: '主机不在线' });
    }

    // 容器更新是异步任务，只返回任务ID
    const taskId = await agentService.sendTask(serverId, {
      type: DockerTaskTypes.DOCKER_UPDATE_CONTAINER,
      data: JSON.stringify({ container_id: containerId, container_name: containerName, image }),
      timeout: 300,
    });

    res.json({
      success: true,
      message: '容器更新任务已启动',
      data: { taskId },
    });
  } catch (error) {
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * Docker 容器重命名
 * POST /docker/container/rename
 * { serverId, containerId, newName }
 */
router.post('/docker/container/rename', async (req, res) => {
  try {
    const { serverId, containerId, newName } = req.body;

    if (!serverId || !containerId || !newName) {
      return res.status(400).json({ success: false, error: '缺少必要参数' });
    }

    if (!agentService.isOnline(serverId)) {
      return res.status(400).json({ success: false, error: '主机不在线' });
    }

    const result = await agentService.sendTaskAndWait(serverId, {
      type: DockerTaskTypes.DOCKER_RENAME_CONTAINER,
      data: JSON.stringify({ container_id: containerId, new_name: newName }),
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
      res.json({ success: true, data: parseJsonSafe(result.data, []) });
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
      res.json({ success: true, data: parseJsonSafe(result.data, []) });
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
      res.json({ success: true, data: parseJsonSafe(result.data, []) });
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
      res.json({ success: true, data: parseJsonSafe(result.data, []) });
    } else {
      res.status(400).json({ success: false, error: result.data });
    }
  } catch (error) {
    res.status(500).json({ success: false, error: error.message });
  }
});

// ==================== Docker Compose 管理 ====================

/**
 * 获取 Docker Compose 项目列表
 * POST /docker/compose/list
 */
router.post('/docker/compose/list', async (req, res) => {
  try {
    const { serverId } = req.body;
    if (!serverId) return res.status(400).json({ success: false, error: '缺少服务器 ID' });
    if (!agentService.isOnline(serverId)) return res.status(400).json({ success: false, error: '主机不在线' });

    const result = await agentService.sendTaskAndWait(serverId, {
      type: DockerTaskTypes.DOCKER_COMPOSE_LIST,
      data: '',
      timeout: 30,
    }, 30000);

    if (result.successful) {
      const projects = parseJsonSafe(result.data, []);
      res.json({ success: true, data: projects });
    } else {
      res.status(400).json({ success: false, error: result.data });
    }
  } catch (error) {
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * Docker Compose 操作 (up/down/restart/pull)
 * POST /docker/compose/action
 */
router.post('/docker/compose/action', async (req, res) => {
  try {
    const { serverId, action, project, configDir } = req.body;
    if (!serverId || !action || !project) return res.status(400).json({ success: false, error: '缺少参数' });
    if (!agentService.isOnline(serverId)) return res.status(400).json({ success: false, error: '主机不在线' });

    const taskData = JSON.stringify({ action, project, config_dir: configDir });
    const timeout = action === 'pull' ? 300 : 120;

    const result = await agentService.sendTaskAndWait(serverId, {
      type: DockerTaskTypes.DOCKER_COMPOSE_ACTION,
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

// ==================== Docker 容器创建 ====================

/**
 * 创建新容器
 * POST /docker/container/create
 */
router.post('/docker/container/create', async (req, res) => {
  try {
    const { serverId, name, image, ports, volumes, env, network, restart, privileged, extraArgs } = req.body;
    if (!serverId || !image) return res.status(400).json({ success: false, error: '缺少服务器 ID 或镜像名称' });
    if (!agentService.isOnline(serverId)) return res.status(400).json({ success: false, error: '主机不在线' });

    const taskData = JSON.stringify({
      name,
      image,
      ports: ports || [],
      volumes: volumes || [],
      env: env || {},
      network: network || '',
      restart: restart || 'unless-stopped',
      privileged: privileged || false,
      extra_args: extraArgs || [],
    });

    const result = await agentService.sendTaskAndWait(serverId, {
      type: DockerTaskTypes.DOCKER_CREATE_CONTAINER,
      data: taskData,
      timeout: 300, // 可能需要拉取镜像
    }, 300000);

    if (result.successful) {
      res.json({ success: true, message: result.data });
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
 * 获取历史指标记录（带后端降采样）
 */
router.get('/metrics/history', (req, res) => {
  try {
    const { serverId, startTime, endTime, page = 1, pageSize = 500, maxPointsPerServer = 100 } = req.query;

    const limit = Math.min(parseInt(pageSize) || 500, 10000);
    const offset = ((parseInt(page) || 1) - 1) * limit;
    const maxPoints = Math.min(parseInt(maxPointsPerServer) || 100, 200);

    let records = ServerMetricsHistory.getHistory({
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

    // 后端降采样：按主机分组，每个主机最多保留 maxPoints 个数据点
    if (records.length > 0) {
      const groupedByServer = {};

      // 按主机分组
      for (const record of records) {
        const sid = record.server_id;
        if (!groupedByServer[sid]) {
          groupedByServer[sid] = [];
        }
        groupedByServer[sid].push(record);
      }

      // 对每个主机进行降采样
      const sampledRecords = [];
      for (const sid of Object.keys(groupedByServer)) {
        const serverRecords = groupedByServer[sid];

        if (serverRecords.length <= maxPoints) {
          // 数据量不多，直接保留
          sampledRecords.push(...serverRecords);
        } else {
          // 需要降采样：均匀选取数据点
          const step = serverRecords.length / maxPoints;
          for (let i = 0; i < maxPoints; i++) {
            const index = Math.floor(i * step);
            sampledRecords.push(serverRecords[index]);
          }
        }
      }

      records = sampledRecords;
    }

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

    const requestedTaskId = require('crypto').randomUUID();
    const taskId = agentService.sendTask(serverId, {
      id: requestedTaskId,
      type: TaskTypes.COMMAND,
      data: command,
      timeout,
    });

    if (!taskId) {
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

      const requestedTaskId = require('crypto').randomUUID();
      const sentTaskId = agentService.sendTask(serverId, {
        id: requestedTaskId,
        type: TaskTypes.COMMAND,
        data: command,
        timeout,
      });

      results.push({
        serverId,
        success: !!sentTaskId,
        taskId: sentTaskId || null,
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

// ==================== SFTP 文件管理 ====================

const sftpService = require('./sftp-service');
// 使用项目已有的 express-fileupload，不需要额外配置

/**
 * 列出目录内容
 * POST /sftp/list
 * { serverId, path }
 */
router.post('/sftp/list', async (req, res) => {
  try {
    // Use '.' as default path to land in home directory
    const { serverId, path = '.' } = req.body;
    if (!serverId) return res.status(400).json({ success: false, error: '缺少服务器 ID' });

    const result = await sftpService.listDirectory(serverId, path);
    // If result has files and cwd, use them. Otherwise assume it's the old array format (unlikely unless service wasn't updated)
    const files = result.files || result;
    const currentPath = result.cwd || path;

    res.json({ success: true, data: files, path: currentPath });
  } catch (error) {
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * 获取文件/目录信息
 * POST /sftp/stat
 * { serverId, path }
 */
router.post('/sftp/stat', async (req, res) => {
  try {
    const { serverId, path } = req.body;
    if (!serverId || !path) return res.status(400).json({ success: false, error: '缺少参数' });

    const stats = await sftpService.stat(serverId, path);
    res.json({ success: true, data: stats });
  } catch (error) {
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * 读取文件内容
 * POST /sftp/read
 * { serverId, path, maxSize? }
 */
router.post('/sftp/read', async (req, res) => {
  try {
    const { serverId, path, maxSize } = req.body;
    if (!serverId || !path) return res.status(400).json({ success: false, error: '缺少参数' });

    const content = await sftpService.readFile(serverId, path, maxSize);
    res.json({ success: true, data: content });
  } catch (error) {
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * 写入文件内容
 * POST /sftp/write
 * { serverId, path, content }
 */
router.post('/sftp/write', async (req, res) => {
  try {
    const { serverId, path, content } = req.body;
    if (!serverId || !path) return res.status(400).json({ success: false, error: '缺少参数' });

    await sftpService.writeFile(serverId, path, content || '');
    res.json({ success: true, message: '文件保存成功' });
  } catch (error) {
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * 创建目录
 * POST /sftp/mkdir
 * { serverId, path }
 */
router.post('/sftp/mkdir', async (req, res) => {
  try {
    const { serverId, path } = req.body;
    if (!serverId || !path) return res.status(400).json({ success: false, error: '缺少参数' });

    await sftpService.mkdir(serverId, path);
    res.json({ success: true, message: '目录创建成功' });
  } catch (error) {
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * 删除文件
 * POST /sftp/delete
 * { serverId, path }
 */
router.post('/sftp/delete', async (req, res) => {
  try {
    const { serverId, path } = req.body;
    if (!serverId || !path) return res.status(400).json({ success: false, error: '缺少参数' });

    await sftpService.deleteFile(serverId, path);
    res.json({ success: true, message: '文件删除成功' });
  } catch (error) {
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * 删除目录
 * POST /sftp/rmdir
 * { serverId, path, recursive? }
 */
router.post('/sftp/rmdir', async (req, res) => {
  try {
    const { serverId, path, recursive } = req.body;
    if (!serverId || !path) return res.status(400).json({ success: false, error: '缺少参数' });

    if (recursive) {
      await sftpService.rmdirRecursive(serverId, path);
    } else {
      await sftpService.rmdir(serverId, path);
    }
    res.json({ success: true, message: '目录删除成功' });
  } catch (error) {
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * 重命名/移动
 * POST /sftp/rename
 * { serverId, oldPath, newPath }
 */
router.post('/sftp/rename', async (req, res) => {
  try {
    const { serverId, oldPath, newPath } = req.body;
    if (!serverId || !oldPath || !newPath) return res.status(400).json({ success: false, error: '缺少参数' });

    await sftpService.rename(serverId, oldPath, newPath);
    res.json({ success: true, message: '重命名成功' });
  } catch (error) {
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * 修改权限
 * POST /sftp/chmod
 * { serverId, path, mode }
 */
router.post('/sftp/chmod', async (req, res) => {
  try {
    const { serverId, path, mode } = req.body;
    if (!serverId || !path || mode === undefined) return res.status(400).json({ success: false, error: '缺少参数' });

    await sftpService.chmod(serverId, path, parseInt(mode, 8));
    res.json({ success: true, message: '权限修改成功' });
  } catch (error) {
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * 下载文件
 * GET /sftp/download/:serverId?path=xxx
 */
router.get('/sftp/download/:serverId', async (req, res) => {
  try {
    const { serverId } = req.params;
    const { path: remotePath } = req.query;

    if (!serverId || !remotePath) {
      return res.status(400).json({ success: false, error: '缺少参数' });
    }

    const { stream, size, filename, conn } = await sftpService.downloadStream(serverId, remotePath);

    res.setHeader('Content-Disposition', `attachment; filename="${encodeURIComponent(filename)}"`);
    res.setHeader('Content-Length', size);
    res.setHeader('Content-Type', 'application/octet-stream');

    stream.pipe(res);

    stream.on('error', err => {
      console.error('Download stream error:', err);
      conn.end();
      if (!res.headersSent) {
        res.status(500).json({ success: false, error: err.message });
      }
    });
  } catch (error) {
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * 上传文件
 * POST /sftp/upload
 * FormData: serverId, path, file
 * 使用 express-fileupload（在 server.js 中全局配置）
 */
router.post('/sftp/upload', async (req, res) => {
  try {
    const { serverId, path: remotePath } = req.body;

    // express-fileupload 将文件放在 req.files 中
    if (!req.files || !req.files.file) {
      return res.status(400).json({ success: false, error: '未找到上传的文件' });
    }

    const file = req.files.file;
    // relativePath 是文件在原文件夹中的相对路径（用于文件夹上传）
    const { relativePath } = req.body;

    if (!serverId || !remotePath) {
      return res.status(400).json({ success: false, error: '缺少 serverId 或 path 参数' });
    }

    // 构建完整的远程文件路径
    let fullPath;
    if (relativePath) {
      // 文件夹上传：使用相对路径
      fullPath = remotePath.endsWith('/')
        ? remotePath + relativePath
        : remotePath + '/' + relativePath;

      // 确保父目录存在
      const parentDir = require('path').posix.dirname(fullPath);
      if (parentDir !== remotePath && parentDir !== '/') {
        await sftpService.mkdirRecursive(serverId, parentDir);
      }
    } else {
      // 普通文件上传
      fullPath = remotePath.endsWith('/')
        ? remotePath + file.name
        : remotePath + '/' + file.name;
    }

    let uploadData = file.data;
    if ((!uploadData || uploadData.length === 0) && file.tempFilePath) {
      uploadData = require('fs').createReadStream(file.tempFilePath);
    }
    if (!uploadData) {
      return res.status(400).json({ success: false, error: '上传文件数据为空' });
    }

    await sftpService.uploadFile(serverId, fullPath, uploadData);
    res.json({ success: true, message: '上传成功', path: fullPath });
  } catch (error) {
    res.status(500).json({ success: false, error: error.message });
  }
});

module.exports = router;
