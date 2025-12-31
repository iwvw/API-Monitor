/**
 * 统一日志工具模块
 * 提供规范化的日志输出，支持不同级别和模块分类
 */

const chalk = require('chalk');
const fs = require('fs');
const path = require('path');
const EventEmitter = require('events');
const { AsyncLocalStorage } = require('async_hooks');

// 创建全局存储，用于追踪请求 Trace ID
const asyncLocalStorage = new AsyncLocalStorage();

// 日志事件发射器，用于实时推送
class LogEmitter extends EventEmitter {}
const logEmitter = new LogEmitter();

// 日志缓存，用于新连接获取历史日志 (挂载到全局以防多模块加载副本)
const LOG_BUFFER_SIZE = 200;
if (!global.__LOG_BUFFER__) {
  global.__LOG_BUFFER__ = [];
}
const logBuffer = global.__LOG_BUFFER__;

// 日志配置 (挂载到全局以便运行时修改)
if (!global.__LOG_CONFIG__) {
  global.__LOG_CONFIG__ = {
    maxFileSizeMB: 10, // 默认 10MB
  };
}
const logConfig = global.__LOG_CONFIG__;

// 日志目录
const LOG_DIR = path.join(process.cwd(), 'data', 'logs');
if (!fs.existsSync(LOG_DIR)) {
  fs.mkdirSync(LOG_DIR, { recursive: true });
}

const LOG_FILE = path.join(LOG_DIR, 'app.log');

// 使用流式写入以提升性能
let logStream = fs.createWriteStream(LOG_FILE, { flags: 'a', encoding: 'utf8' });

/**
 * 获取日志配置
 */
function getLogConfig() {
  return { ...logConfig };
}

/**
 * 更新日志配置
 */
function updateLogConfig(config) {
  if (config.maxFileSizeMB !== undefined) {
    logConfig.maxFileSizeMB = Math.max(1, parseInt(config.maxFileSizeMB) || 10);
  }
  return getLogConfig();
}

/**
 * 获取当前日志文件信息
 */
function getLogFileInfo() {
  try {
    if (fs.existsSync(LOG_FILE)) {
      const stats = fs.statSync(LOG_FILE);
      return {
        size: stats.size,
        sizeMB: (stats.size / (1024 * 1024)).toFixed(2),
        maxSizeMB: logConfig.maxFileSizeMB,
        usagePercent: ((stats.size / (logConfig.maxFileSizeMB * 1024 * 1024)) * 100).toFixed(1),
        modifiedAt: stats.mtime.toISOString(),
        path: LOG_FILE,
      };
    }
    return {
      size: 0,
      sizeMB: '0.00',
      maxSizeMB: logConfig.maxFileSizeMB,
      usagePercent: '0.0',
      path: LOG_FILE,
    };
  } catch (e) {
    return {
      size: 0,
      sizeMB: '0.00',
      maxSizeMB: logConfig.maxFileSizeMB,
      usagePercent: '0.0',
      path: LOG_FILE,
      error: e.message,
    };
  }
}

/**
 * 物理清空日志文件并重建流
 */
function clearLogFile() {
  try {
    logStream.end(); // 关闭当前流
    fs.writeFileSync(LOG_FILE, '', 'utf8'); // 清空文件
    logStream = fs.createWriteStream(LOG_FILE, { flags: 'a', encoding: 'utf8' }); // 重新打开
    global.__LOG_BUFFER__ = []; // 同时清空内存缓存
    return true;
  } catch (e) {
    console.error('Failed to clear log file:', e);
    return false;
  }
}

/**
 * 检查并自动清理日志 (超过配置的最大大小则清空)
 */
function checkSizeAndRotation() {
  try {
    if (fs.existsSync(LOG_FILE)) {
      const stats = fs.statSync(LOG_FILE);
      const maxSize = logConfig.maxFileSizeMB * 1024 * 1024;
      if (maxSize > 0 && stats.size > maxSize) {
        console.log(
          `Log file (${(stats.size / (1024 * 1024)).toFixed(2)}MB) exceeds limit (${logConfig.maxFileSizeMB}MB), auto-clearing...`
        );
        clearLogFile();
      }
    }
  } catch (e) {}
}

// 日志级别
const LOG_LEVELS = {
  DEBUG: 0,
  INFO: 1,
  WARN: 2,
  ERROR: 3,
  FATAL: 4,
  SILENT: 5,
};

// 当前日志级别（从环境变量读取，默认为INFO）
const currentLevel = LOG_LEVELS[process.env.LOG_LEVEL?.toUpperCase()] ?? LOG_LEVELS.INFO;

// 是否启用彩色输出
const useColor = process.env.NO_COLOR !== '1';

// 格式化时间戳
function getTimestamp() {
  return new Date().toISOString();
}

function formatDisplayTimestamp(isoString) {
  const now = new Date(isoString);
  const hours = String(now.getHours()).padStart(2, '0');
  const minutes = String(now.getMinutes()).padStart(2, '0');
  const seconds = String(now.getSeconds()).padStart(2, '0');
  const ms = String(now.getMilliseconds()).padStart(3, '0');
  return `${hours}:${minutes}:${seconds}.${ms}`;
}

// 敏感数据脱敏
function maskSensitiveInfo(data) {
  if (!data) return data;

  if (typeof data === 'string') {
    // 基础字符串脱敏
    return data.replace(
      /(token|password|key|secret|api_key|apiToken)(["']?\s*[:=]\s*["']?)([^"'\s&,]+)/gi,
      '$1$2******'
    );
  }

  if (typeof data === 'object' && data !== null) {
    // 递归对象脱敏
    try {
      const masked = Array.isArray(data) ? [] : {};
      for (const key in data) {
        if (Object.prototype.hasOwnProperty.call(data, key)) {
          const lowerKey = key.toLowerCase();
          const isSensitive =
            lowerKey.includes('token') ||
            lowerKey.includes('password') ||
            lowerKey.includes('key') ||
            lowerKey.includes('secret') ||
            lowerKey.includes('credential');

          if (isSensitive) {
            masked[key] = '******';
          } else if (typeof data[key] === 'object') {
            masked[key] = maskSensitiveInfo(data[key]);
          } else {
            masked[key] = data[key];
          }
        }
      }
      return masked;
    } catch (e) {
      return '[Circular or Error Data]';
    }
  }

  return data;
}

/**
 * 日志输出核心函数
 */
function log(level, module, message, data) {
  if (LOG_LEVELS[level] < currentLevel) return;

  const timestamp = getTimestamp();
  const context = asyncLocalStorage.getStore() || {};
  const traceId = context.traceId || '';

  // 脱敏处理
  const maskedMessage = maskSensitiveInfo(message);
  const maskedData = maskSensitiveInfo(data);

  // 自动检查大小
  checkSizeAndRotation();

  // 1. 终端渲染 (用于开发调试)
  renderTerminal(level, module, timestamp, traceId, maskedMessage, maskedData);

  // 2. 构造结构化日志对象
  const logEntry = {
    timestamp,
    level,
    traceId,
    module: module || 'core',
    message: maskedMessage,
    data: maskedData,
  };

  // 3. 内存缓冲区更新
  logBuffer.push(logEntry);
  if (logBuffer.length > LOG_BUFFER_SIZE) {
    logBuffer.shift();
  }

  // 4. 持久化到文件 (JSON 格式)
  const logLine = JSON.stringify(logEntry) + '\n';
  logStream.write(logLine);

  // 5. 实时推送事件
  logEmitter.emit('log', logEntry);
}

function getModuleColor(module) {
  if (!useColor) return t => t;
  const mod = (module || 'core').toLowerCase();

  // 语义化配色映射
  if (mod.includes('servermoni')) return chalk.green; // 监控 - 绿色
  if (mod.includes('ssh')) return chalk.white.bold; // SSH - 粗体白
  if (mod.includes('zeabur') || mod.includes('paas')) return chalk.cyan; // PaaS - 青色
  if (mod.includes('antigravit')) return chalk.magenta; // Antigravity - 品红
  if (mod.includes('gemini')) return chalk.blueBright; // Gemini - 亮蓝
  if (mod.includes('openai')) return chalk.greenBright; // OpenAI - 亮绿
  if (mod.includes('dns') || mod.includes('cloudflar')) return chalk.orange || chalk.yellow; // DNS - 橙/黄
  if (mod.includes('auth')) return chalk.redBright; // 认证 - 亮红
  if (mod.includes('database') || mod.includes('db')) return chalk.yellow; // 数据库 - 黄色
  if (mod.includes('http')) return chalk.blue; // HTTP - 蓝色
  if (mod.includes('log')) return chalk.magentaBright; // 日志服务 - 亮紫
  if (mod.includes('session')) return chalk.gray; // 会话 - 灰色

  return chalk.cyanBright; // 默认颜色
}

function renderTerminal(level, module, timestamp, traceId, message, data) {
  const levelColors = {
    DEBUG: useColor ? chalk.gray : t => t,
    INFO: useColor ? chalk.blue : t => t,
    WARN: useColor ? chalk.yellow : t => t,
    ERROR: useColor ? chalk.red : t => t,
    FATAL: useColor ? chalk.bgRed.white.bold : t => t,
  };

  const colorFn = levelColors[level] || (t => t);
  const displayTime = formatDisplayTimestamp(timestamp);

  // 固定宽度定义，确保完美对齐
  const COL_SYSTEM = 4; // [0]
  const COL_TIME = 13; // HH:mm:ss.SSS
  const COL_LEVEL = 6; // ERROR
  const COL_MODULE = 13; // [ModuleName]

  // 1. 系统 ID (默认为 [0])
  const sysStr = useColor ? chalk.gray('[0]'.padEnd(COL_SYSTEM)) : '[0]'.padEnd(COL_SYSTEM);

  // 2. 时间戳
  const timeStr = useColor
    ? chalk.gray(displayTime.padEnd(COL_TIME))
    : displayTime.padEnd(COL_TIME);

  // 3. 级别
  const levelStr = colorFn(level.padEnd(COL_LEVEL));

  // 4. 模块名 (动态配色)
  const rawModule = (module || 'core').substring(0, 10);
  const formattedModule = `[${rawModule}]`.padEnd(COL_MODULE);
  const moduleColorFn = getModuleColor(module);
  const moduleStr = moduleColorFn(formattedModule);

  // 组合输出
  const output = `${sysStr} ${timeStr} ${levelStr} ${moduleStr} ${message}`;
  console.log(output);

  if (data !== undefined && level !== 'INFO') {
    const indent = ' '.repeat(COL_SYSTEM + COL_TIME + COL_LEVEL + COL_MODULE + 4);
    if (typeof data === 'object') {
      try {
        const json = JSON.stringify(data, null, 2)
          .split('\n')
          .map(line => indent + line)
          .join('\n');
        console.log(colorFn(json));
      } catch (e) {
        console.log(colorFn(indent + '[Complex Data]'));
      }
    } else {
      console.log(colorFn(indent + data));
    }
  }
}

/**
 * 创建模块化的日志器
 */
function createLogger(moduleName) {
  return {
    debug: (message, data) => log('DEBUG', moduleName, message, data),
    info: (message, data) => log('INFO', moduleName, message, data),
    warn: (message, data) => log('WARN', moduleName, message, data),
    error: (message, data) => log('ERROR', moduleName, message, data),
    fatal: (message, data) => log('FATAL', moduleName, message, data),

    success: (message, data) => {
      const msg = useColor ? chalk.green('✓ ' + message) : '✓ ' + message;
      log('INFO', moduleName, msg, data);
    },

    start: message => {
      const msg = useColor ? chalk.cyan('▶ ' + message) : '▶ ' + message;
      log('INFO', moduleName, msg);
    },

    complete: (message, data) => {
      const msg = useColor ? chalk.green('✓ ' + message) : '✓ ' + message;
      log('INFO', moduleName, msg, data);
    },

    // 恢复这些方法以兼容现有代码，防止报错
    group: title => {
      const msg = useColor ? chalk.bold(title) : title;
      log('INFO', moduleName, msg);
    },

    groupItem: (message, data) => {
      const msg = '  • ' + message;
      log('INFO', moduleName, msg, data);
    },
  };
}

const globalLogger = createLogger('');

module.exports = {
  createLogger,
  logger: globalLogger,
  logEmitter,
  getBuffer: () => global.__LOG_BUFFER__ || [],
  clearLogFile,
  LOG_FILE,
  asyncLocalStorage,
  LOG_LEVELS,
  // 新增日志配置管理
  getLogConfig,
  updateLogConfig,
  getLogFileInfo,
};
