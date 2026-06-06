/**
 * Agent-Dashboard 通信协议定义
 */

// ==================== 事件类型 ====================

const net = require('net');

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
  DOCKER_COMPOSE_LIST: 21, // Docker Compose 项目列表
  DOCKER_COMPOSE_ACTION: 22, // Docker Compose 操作 (up/down/restart)
  DOCKER_CREATE_CONTAINER: 23, // 创建新容器
  DOCKER_UPDATE_CONTAINER: 24, // 容器一键更新
  DOCKER_RENAME_CONTAINER: 25, // 容器重命名
  DOCKER_TASK_PROGRESS: 26, // 查询任务进度

  // 文件管理 (Agent 原生，无需 SSH/SFTP)
  FILE_LIST: 30, // 列出目录
  FILE_READ: 31, // 读取文件
  FILE_WRITE: 32, // 写入文件
  FILE_MKDIR: 33, // 创建目录
  FILE_DELETE: 34, // 删除文件/目录
  FILE_RENAME: 35, // 重命名/移动
  FILE_STAT: 36, // 获取文件信息
  FILE_CHMOD: 37, // 修改权限
  FILE_DOWNLOAD_CHUNK: 38, // 分块下载文件
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
  cpu_temp: 0, // CPU 温度 (摄氏度)
  gpu_temp: 0, // GPU 温度 (摄氏度)
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
function toFiniteNumber(value, defaultVal = 0) {
  const num = Number(value);
  return Number.isFinite(num) ? num : defaultVal;
}

function parseByteValue(value, defaultVal = 0) {
  if (typeof value === 'number') {
    return Number.isFinite(value) && value > 0 ? value : defaultVal;
  }

  if (typeof value !== 'string') return defaultVal;

  const raw = value.trim();
  if (!raw || raw === '-' || raw.toLowerCase() === 'nan') return defaultVal;

  const match = raw.replace(/,/g, '').match(/^([0-9]+(?:\.[0-9]+)?)\s*([KMGTPE]?I?B?)?(?:\/s)?$/i);
  if (!match) return defaultVal;

  const amount = Number(match[1]);
  if (!Number.isFinite(amount)) return defaultVal;

  const unit = (match[2] || 'B').toUpperCase().replace('IB', 'B');
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB', 'EB'];
  const normalizedUnit = unit.length === 1 && unit !== 'B' ? `${unit}B` : unit;
  const power = units.indexOf(normalizedUnit);

  return amount * Math.pow(1024, power >= 0 ? power : 0);
}

function formatBytes(bytes, decimals = 2) {
  bytes = Math.max(0, toFiniteNumber(bytes, 0));
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.min(sizes.length - 1, Math.floor(Math.log(bytes) / Math.log(k)));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(decimals)) + ' ' + sizes[i];
}

function normalizeByteText(value, fallback = '0 B') {
  const bytes = parseByteValue(value, null);
  if (bytes === null) return fallback;
  return formatBytes(bytes);
}

function sanitizeIp(value) {
  if (typeof value !== 'string') return '';
  const ip = value.trim();
  return net.isIP(ip) ? ip : '';
}

function sanitizeHostInfo(hostInfo = {}) {
  if (!hostInfo || typeof hostInfo !== 'object') return {};
  return {
    ...hostInfo,
    ip: sanitizeIp(hostInfo.ip),
  };
}

function normalizeNetworkMetrics(network = {}) {
  const source = network && typeof network === 'object' ? network : {};
  return {
    ...source,
    rx_speed: source.rx_speed || source.down || '0 B/s',
    tx_speed: source.tx_speed || source.up || '0 B/s',
    down: source.down || source.rx_speed || '0 B/s',
    up: source.up || source.tx_speed || '0 B/s',
    rx_total: normalizeByteText(source.rx_total),
    tx_total: normalizeByteText(source.tx_total),
    connections: Math.max(0, Math.round(toFiniteNumber(source.connections, 0))),
  };
}

function parsePercentValue(value, defaultVal = null) {
  if (value === null || value === undefined || value === '') return defaultVal;
  const parsed = typeof value === 'number' ? value : parseFloat(String(value));
  return Number.isFinite(parsed) ? parsed : defaultVal;
}

function resolveGpuMemoryPercent(metrics = {}) {
  if (!metrics || typeof metrics !== 'object') return 0;

  const explicitPercent = parsePercentValue(metrics.gpu_mem_percent ?? metrics.gpu?.Percent);
  if (explicitPercent !== null) {
    return Math.max(0, Math.min(100, explicitPercent));
  }

  const used = parseByteValue(metrics.gpu_mem_used, null);
  const total = parseByteValue(metrics.gpu_mem_total, null);
  if (used !== null && total !== null && total > 0) {
    return Math.max(0, Math.min(100, (used / total) * 100));
  }

  if (typeof metrics.gpu_mem === 'string' && metrics.gpu_mem.includes('/')) {
    const [rawUsed, rawTotal] = metrics.gpu_mem.split('/');
    const textUsed = parseByteValue(rawUsed, null);
    const textTotal = parseByteValue(rawTotal, null);
    if (textUsed !== null && textTotal !== null && textTotal > 0) {
      return Math.max(0, Math.min(100, (textUsed / textTotal) * 100));
    }
  }

  return 0;
}

function buildGpuInfo(metrics = {}) {
  const source = metrics && typeof metrics === 'object' ? metrics : {};
  const modelFromArray = Array.isArray(source.gpu) ? source.gpu.filter(Boolean).join(' / ') : '';
  return {
    Model: source.gpu_model || source.gpu?.Model || modelFromArray || '',
    Usage: source.gpu_usage || source.gpu?.Usage || '0%',
    Memory: source.gpu_mem || source.gpu?.Memory || '',
    Power: source.gpu_power || source.gpu?.Power || '',
    Temp: source.gpu_temp !== undefined ? source.gpu_temp : source.gpu?.Temp,
    Percent: resolveGpuMemoryPercent(source),
  };
}

function parseTemperatureValue(value) {
  if (value === null || value === undefined || value === '') return null;
  const parsed = typeof value === 'number'
    ? value
    : parseFloat(String(value).replace(/[^\d.-]/g, ''));
  if (!Number.isFinite(parsed) || parsed <= 0 || parsed > 130) return null;
  return parsed;
}

function collectTemperatureReadings(input, parentName = '') {
  if (input === null || input === undefined) return [];
  const readings = [];

  const entries = Array.isArray(input)
    ? input.map(sensor => ({ key: '', sensor }))
    : typeof input === 'object'
      ? Object.entries(input).map(([key, sensor]) => ({ key, sensor }))
      : [{ key: '', sensor: input }];

  const scalarKeys = new Set([
    'name',
    'Name',
    'label',
    'Label',
    'sensor',
    'Sensor',
    'type',
    'Type',
    'temperature',
    'Temperature',
    'temp',
    'Temp',
    'current',
    'Current',
    'value',
    'Value',
    'entries',
    'Sensors',
    'sensors',
    'values',
    'children',
  ]);

  for (const { key, sensor } of entries) {
    if (sensor === null || sensor === undefined) continue;

    const keyName = Number.isInteger(Number(key)) ? '' : key;
    const scopedName = [parentName, keyName].filter(Boolean).join(' ');

    if (typeof sensor !== 'object') {
      const value = parseTemperatureValue(sensor);
      if (value !== null) readings.push({ name: scopedName, value });
      continue;
    }

    const ownName = [
      scopedName,
      sensor.name ?? sensor.Name ?? sensor.label ?? sensor.Label ?? sensor.sensor ?? sensor.Sensor ?? sensor.type ?? sensor.Type,
    ].filter(Boolean).join(' ');
    const value = parseTemperatureValue(
      sensor.temperature ?? sensor.Temperature ?? sensor.temp ?? sensor.Temp ?? sensor.current ?? sensor.Current ?? sensor.value ?? sensor.Value,
    );
    if (value !== null) readings.push({ name: ownName, value });

    for (const key of ['entries', 'Sensors', 'sensors', 'values', 'children']) {
      readings.push(...collectTemperatureReadings(sensor[key], ownName));
    }

    for (const [nestedKey, nestedValue] of Object.entries(sensor)) {
      if (scalarKeys.has(nestedKey)) continue;
      if (nestedValue && typeof nestedValue === 'object') {
        readings.push(...collectTemperatureReadings(nestedValue, [ownName, nestedKey].filter(Boolean).join(' ')));
      }
    }
  }

  return readings;
}

function getCpuTemperatureRank(name) {
  const normalized = String(name || '').toLowerCase();
  if (/gpu|nvidia|radeon|nvme|ssd|hdd|disk|drive|battery|fan|ambient/.test(normalized)) return 0;
  if (/package|tctl|tdie|x86_pkg|cpu package/.test(normalized)) return 5;
  if (/\bcpu\b|cpu_thermal/.test(normalized)) return 4;
  if (/core\s*\d+|coretemp|k10temp/.test(normalized)) return 3;
  if (/thermal/.test(normalized)) return 1;
  return 0;
}

function resolveCpuTemperature(metrics = {}) {
  if (!metrics || typeof metrics !== 'object') return 0;

  const explicitSources = [
    metrics.cpu_temp,
    metrics.cpuTemp,
    metrics.cpu_temperature,
    metrics.cpuTemperature,
    metrics.cpu_temperature_celsius,
    metrics.cpuTemperatureCelsius,
    metrics.cpu_temp_c,
    metrics.cpu?.Temperature,
    metrics.cpu?.Temp,
    metrics.cpu?.temp,
    metrics.cpu?.temperature,
  ];
  for (const source of explicitSources) {
    const explicit = parseTemperatureValue(source);
    if (explicit !== null) return explicit;
  }

  const readings = [
    ...collectTemperatureReadings(metrics.temperatures),
    ...collectTemperatureReadings(metrics.temperature_sensors),
    ...collectTemperatureReadings(metrics.temperatureSensors),
    ...collectTemperatureReadings(metrics.sensors),
    ...collectTemperatureReadings(metrics.thermal),
  ];
  const ranked = readings
    .map(reading => ({ ...reading, rank: getCpuTemperatureRank(reading.name) }))
    .filter(reading => reading.rank > 0)
    .sort((a, b) => (b.rank - a.rank) || (b.value - a.value));

  if (ranked.length > 0) return ranked[0].value;

  const usable = readings.filter(reading => getCpuTemperatureRank(reading.name) !== 0);
  return usable.length === 1 ? usable[0].value : 0;
}

function normalizeFrontendMetrics(metrics = {}) {
  if (!metrics || typeof metrics !== 'object') return metrics;
  const normalized = {
    ...metrics,
    ip: sanitizeIp(metrics.ip),
  };

  if (metrics.network) {
    normalized.network = normalizeNetworkMetrics(metrics.network);
  }

  normalized.cpu_temp = resolveCpuTemperature(normalized);
  normalized.gpu_mem_percent = resolveGpuMemoryPercent(normalized);

  return normalized;
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
  const safeNumber = (val, defaultVal = 0) => toFiniteNumber(val, defaultVal);

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
  const cpuTemp = resolveCpuTemperature(state);
  const gpuTemp = safeNumber(state.gpu_temp || state.gpuTemp);

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

  const cores = (() => {
    const explicit = safeNumber(hostInfo.cores || hostInfo.Cores);
    if (explicit > 0) return explicit;
    // 尝试从 CPU 描述字符串中解析核心数 (例如 "Intel ... 12 Core(s)")
    if (hostInfo.cpu && hostInfo.cpu.length > 0) {
      const match = hostInfo.cpu[0].match(/(\d+)\s*Core/i);
      if (match) return parseInt(match[1]) || 1;
    }
    return 0;
  })();

  const logicalCores = safeNumber(hostInfo.logical_cores || hostInfo.LogicalCores) || cores;
  const physicalCores = safeNumber(hostInfo.physical_cores || hostInfo.PhysicalCores) || cores;

  return {
    cpu_usage: cpu.toFixed(1) + '%',
    load: `${load1.toFixed(2)} ${load5.toFixed(2)} ${load15.toFixed(2)}`,
    cores,
    logical_cores: logicalCores,
    physical_cores: physicalCores,
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
    network: normalizeNetworkMetrics({
      rx_speed: formatSpeed(netInSpeed),
      tx_speed: formatSpeed(netOutSpeed),
      down: formatSpeed(netInSpeed),
      up: formatSpeed(netOutSpeed),
      rx_total: formatBytes(netInTransfer),
      tx_total: formatBytes(netOutTransfer),
      connections: tcpConn + udpConn,
    }),
    docker: state.docker || { installed: false, running: 0, stopped: 0, containers: [] },
    cpu_temp: cpuTemp,
    gpu_temp: gpuTemp,
    gpu: safeNumber(state.gpu),
    gpu_usage: safeNumber(state.gpu).toFixed(1) + '%',
    // 当 GPU 显存总量无效 (<= 1024 bytes, 即没有真实数据) 时不显示
    gpu_mem: gpuMemTotal > 1024 ? `${formatBytes(gpuMemUsed)}/${formatBytes(gpuMemTotal)}` : '',
    gpu_mem_used: gpuMemUsed,
    gpu_mem_total: gpuMemTotal,
    gpu_mem_percent: gpuMemTotal > 1024 ? gpuMemPercent : 0,
    gpu_power: safeNumber(state.gpu_power).toFixed(0) + 'W',
    gpu_model: Array.isArray(hostInfo.gpu) && hostInfo.gpu.length > 0 ? hostInfo.gpu.filter(Boolean).join(' / ') : '',
    platform: hostInfo.platform || '',
    platformVersion: hostInfo.platform_version || hostInfo.platformVersion || '',
    agent_version: hostInfo.agent_version || '',
    ip: sanitizeIp(hostInfo.ip),
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
  parseByteValue,
  normalizeByteText,
  normalizeNetworkMetrics,
  normalizeFrontendMetrics,
  resolveCpuTemperature,
  resolveGpuMemoryPercent,
  buildGpuInfo,
  sanitizeIp,
  sanitizeHostInfo,
  validateHostState,
  stateToFrontendFormat,
};
