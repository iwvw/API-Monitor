/**
 * Agent-Dashboard 通信协议定义
 */

// ==================== 事件类型 ====================

const Events = {
  // Agent -> Dashboard
  AGENT_CONNECT: 'agent:connect', // Agent 连接认证
  AGENT_HOST_INFO: 'agent:host_info', // 上报主机硬件信息
  AGENT_STATE: 'agent:state', // 上报实时状态 (每 1-2 秒)
  AGENT_TASK_RESULT: 'agent:task_result', // 任务执行结果
  AGENT_DISCONNECT: 'agent:disconnect', // Agent 主动断开

  // Dashboard -> Agent
  DASHBOARD_AUTH_OK: 'dashboard:auth_ok', // 认证成功
  DASHBOARD_AUTH_FAIL: 'dashboard:auth_fail', // 认证失败
  DASHBOARD_TASK: 'dashboard:task', // 下发任务
  DASHBOARD_PING: 'dashboard:ping', // 心跳检测
  DASHBOARD_PTY_INPUT: 'dashboard:pty_input', // PTY 输入流
  DASHBOARD_PTY_RESIZE: 'dashboard:pty_resize', // PTY 窗口缩放
  AGENT_PTY_DATA: 'agent:pty_data', // PTY 输出流

  // Dashboard -> Frontend (房间广播)
  METRICS_UPDATE: 'metrics:update', // 单个主机指标更新
  METRICS_BATCH: 'metrics:batch', // 批量指标更新
  SERVER_STATUS: 'server:status', // 主机状态变更 (上线/离线)
  SERVER_LIST: 'server:list', // 完整主机列表
};

// ==================== 任务类型 ====================

const TaskTypes = {
  COMMAND: 1, // 执行命令
  TERMINAL: 2, // 终端会话
  FILE_DOWNLOAD: 3, // 文件下载
  FILE_UPLOAD: 4, // 文件上传
  UPGRADE: 5, // Agent 升级
  REPORT_HOST_INFO: 6, // 请求上报主机信息
  KEEPALIVE: 7, // 心跳保活
  DOCKER_ACTION: 10, // Docker 容器操作
  DOCKER_CHECK_UPDATE: 11, // Docker 检查更新
  PTY_START: 12, // 启动 PTY 终端
  DOCKER_IMAGES: 13, // Docker 镜像列表
  DOCKER_IMAGE_ACTION: 14, // Docker 镜像操作 (pull/remove/prune)
  DOCKER_NETWORKS: 15, // Docker 网络列表
  DOCKER_NETWORK_ACTION: 16, // Docker 网络操作
  DOCKER_VOLUMES: 17, // Docker Volume 列表
  DOCKER_VOLUME_ACTION: 18, // Docker Volume 操作
  DOCKER_LOGS: 19, // Docker 容器日志
  DOCKER_STATS: 20, // Docker 容器资源统计
};

// ==================== 数据结构 ====================

/**
 * @typedef {Object} HostInfo
 */
const HostInfoSchema = {
  platform: '', // 'linux', 'windows', 'darwin'
  platform_version: '', // 'Ubuntu 22.04', 'Windows 11'
  cpu: [], // ['Intel i7-12700 12 Physical Core']
  gpu: [], // ['NVIDIA RTX 4090']
  mem_total: 0, // 总内存 (bytes)
  disk_total: 0, // 总磁盘 (bytes)
  swap_total: 0, // 总交换空间 (bytes)
  arch: '', // 'x86_64', 'aarch64', 'arm'
  virtualization: '', // 'kvm', 'docker', 'vmware', ''
  boot_time: 0, // 系统启动时间 (Unix timestamp)
  ip: '', // 公网 IP
  country_code: '', // 国家代码 (可选)
  agent_version: '', // Agent 版本号
};

/**
 * @typedef {Object} HostState
 */
const HostStateSchema = {
  cpu: 0, // CPU 使用率 (0-100)
  mem_used: 0, // 已用内存 (bytes)
  swap_used: 0, // 已用交换空间 (bytes)
  disk_used: 0, // 已用磁盘 (bytes)
  net_in_transfer: 0, // 入站流量累计 (bytes)
  net_out_transfer: 0, // 出站流量累计 (bytes)
  net_in_speed: 0, // 入站速度 (bytes/s)
  net_out_speed: 0, // 出站速度 (bytes/s)
  uptime: 0, // 运行时长 (seconds)
  load1: 0, // 1 分钟负载
  load5: 0, // 5 分钟负载
  load15: 0, // 15 分钟负载
  tcp_conn_count: 0, // TCP 连接数
  udp_conn_count: 0, // UDP 连接数
  process_count: 0, // 进程数
  temperatures: [], // 温度传感器 [{ name, temperature }]
  gpu: 0, // GPU 使用率 (0-100)
  docker: {
    installed: false,
    running: 0,
    stopped: 0,
    containers: [], // [{ id, name, image, status, created }]
  },
};

/**
 * Agent 连接请求
 * @typedef {Object} AgentConnectRequest
 */
const AgentConnectRequestSchema = {
  server_id: '', // 主机 ID (UUID 或数据库 ID)
  key: '', // 全局 Agent 密钥
  hostname: '', // 主机名 (可选，用于自动注册)
  version: '', // Agent 版本
};

/**
 * 任务定义
 * @typedef {Object} Task
 */
const TaskSchema = {
  id: '', // 任务 ID
  type: 0, // 任务类型 (TaskTypes)
  data: '', // 任务数据 (JSON 字符串或命令)
  timeout: 0, // 超时时间 (秒, 0 表示无限)
};

/**
 * 任务结果
 * @typedef {Object} TaskResult
 */
const TaskResultSchema = {
  id: '', // 任务 ID
  type: 0, // 任务类型
  successful: false, // 是否成功
  data: '', // 执行结果或错误信息
  delay: 0, // 执行耗时 (毫秒)
};

// ==================== 工具函数 ====================

/**
 * 格式化字节数为人类可读格式
 * @param {number} bytes
 * @param {number} decimals
 * @returns {string}
 */
function formatBytes(bytes, decimals = 2) {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(decimals)) + ' ' + sizes[i];
}

/**
 * 格式化速度为人类可读格式
 * @param {number} bytesPerSecond
 * @returns {string}
 */
function formatSpeed(bytesPerSecond) {
  return formatBytes(bytesPerSecond) + '/s';
}

/**
 * 格式化运行时长
 * @param {number} seconds
 * @returns {string}
 */
function formatUptime(seconds) {
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);

  if (days > 0) {
    return `${days}d ${hours}h`;
  } else if (hours > 0) {
    return `${hours}h ${minutes}m`;
  } else {
    return `${minutes}m`;
  }
}

/**
 * 验证 HostState 数据结构
 * @param {Object} state
 * @returns {boolean}
 */
function validateHostState(state) {
  if (!state || typeof state !== 'object') return false;
  if (typeof state.cpu !== 'number') return false;
  if (typeof state.mem_used !== 'number') return false;
  return true;
}

/**
 * 将 HostState 转换为前端友好格式
 * @param {Object} state - HostState
 * @param {Object} hostInfo - HostInfo
 * @returns {Object}
 */
function stateToFrontendFormat(state, hostInfo = {}) {
  // 确保数值有效
  const safeNumber = (val, defaultVal = 0) => {
    const num = Number(val);
    return isNaN(num) || !isFinite(num) ? defaultVal : num;
  };

  const cpu = safeNumber(state.cpu);
  const memUsed = safeNumber(state.mem_used);
  const memTotal = safeNumber(hostInfo.mem_total) || 1;
  const diskUsed = safeNumber(state.disk_used);
  const diskTotal = safeNumber(hostInfo.disk_total) || 1;
  const load1 = safeNumber(state.load1);
  const load5 = safeNumber(state.load5);
  const load15 = safeNumber(state.load15);
  const netInSpeed = safeNumber(state.net_in_speed);
  const netOutSpeed = safeNumber(state.net_out_speed);
  const netInTransfer = safeNumber(state.net_in_transfer);
  const netOutTransfer = safeNumber(state.net_out_transfer);
  const tcpConn = safeNumber(state.tcp_conn_count);
  const udpConn = safeNumber(state.udp_conn_count);
  const uptime = safeNumber(state.uptime);

  // GPU 显存
  const gpuMemUsed = safeNumber(state.gpu_mem_used);
  const gpuMemTotal = safeNumber(hostInfo.gpu_mem_total || state.gpu_mem_total) || 1;
  const gpuMemPercent = gpuMemTotal > 0 ? Math.min(100, (gpuMemUsed / gpuMemTotal) * 100) : 0;

  // 计算百分比
  const memPercent = memTotal > 0 ? Math.min(100, (memUsed / memTotal) * 100) : 0;
  const diskPercent = diskTotal > 0 ? Math.min(100, (diskUsed / diskTotal) * 100) : 0;

  // 转换为 MB
  const memUsedMB = Math.round(memUsed / 1024 / 1024);
  const memTotalMB = Math.round(memTotal / 1024 / 1024);

  return {
    cpu_usage: cpu.toFixed(1) + '%',
    load: `${load1.toFixed(2)} ${load5.toFixed(2)} ${load15.toFixed(2)}`,
    cores: (() => {
      const explicit = safeNumber(hostInfo.cores || hostInfo.Cores);
      if (explicit > 0) return explicit;
      // 尝试从 CPU 描述字符串中解析核心数 (例如 "Intel ... 12 Core(s)")
      if (hostInfo.cpu && hostInfo.cpu.length > 0) {
        const match = hostInfo.cpu[0].match(/(\d+)\s*Core/i);
        if (match) return parseInt(match[1]) || 1;
      }
      return 0;
    })(),
    // 保持前端兼容的格式: "使用量/总量MB"
    mem: `${memUsedMB}/${memTotalMB}MB`,
    mem_usage: `${memUsedMB}/${memTotalMB}MB`,
    mem_percent: memPercent,
    // 磁盘也保持原格式: "已用/总量 (百分比%)"
    disk: `${formatBytes(diskUsed)}/${formatBytes(diskTotal)} (${diskPercent.toFixed(0)}%)`,
    disk_used: formatBytes(diskUsed),
    disk_total: formatBytes(diskTotal),
    disk_usage: `${formatBytes(diskUsed)}/${formatBytes(diskTotal)} (${diskPercent.toFixed(0)}%)`,
    disk_percent: diskPercent,
    network: {
      rx_speed: formatSpeed(netInSpeed),
      tx_speed: formatSpeed(netOutSpeed),
      rx_total: formatBytes(netInTransfer),
      tx_total: formatBytes(netOutTransfer),
      connections: tcpConn + udpConn,
    },
    docker: state.docker || { installed: false, running: 0, stopped: 0, containers: [] },
    gpu: safeNumber(state.gpu),
    gpu_usage: safeNumber(state.gpu).toFixed(1) + '%',
    // 当 GPU 显存总量无效 (<= 1024 bytes, 即没有真实数据) 时不显示
    gpu_mem: gpuMemTotal > 1024 ? `${formatBytes(gpuMemUsed)}/${formatBytes(gpuMemTotal)}` : '',
    gpu_mem_used: gpuMemUsed,
    gpu_mem_total: gpuMemTotal,
    gpu_mem_percent: gpuMemTotal > 1024 ? gpuMemPercent : 0,
    gpu_power: safeNumber(state.gpu_power).toFixed(0) + 'W',
    gpu_model: Array.isArray(hostInfo.gpu) && hostInfo.gpu.length > 0 ? hostInfo.gpu[0] : '',
    platform: hostInfo.platform || '',
    platformVersion: hostInfo.platform_version || hostInfo.platformVersion || '',
    agent_version: hostInfo.agent_version || '',
    uptime: formatUptime(uptime),
    timestamp: Date.now(),
  };
}

module.exports = {
  Events,
  TaskTypes,
  HostInfoSchema,
  HostStateSchema,
  AgentConnectRequestSchema,
  TaskSchema,
  TaskResultSchema,
  formatBytes,
  formatSpeed,
  formatUptime,
  validateHostState,
  stateToFrontendFormat,
};
