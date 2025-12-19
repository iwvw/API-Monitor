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

// 日志缓存，用于新连接获取历史日志
const LOG_BUFFER_SIZE = 200;
const logBuffer = [];

// 日志目录
const LOG_DIR = path.join(process.cwd(), 'data', 'logs');
if (!fs.existsSync(LOG_DIR)) {
  fs.mkdirSync(LOG_DIR, { recursive: true });
}

const LOG_FILE = path.join(LOG_DIR, 'app.log');

// 使用流式写入以提升性能
const logStream = fs.createWriteStream(LOG_FILE, { flags: 'a', encoding: 'utf8' });

// 日志级别
const LOG_LEVELS = {
  DEBUG: 0,
  INFO: 1,
  WARN: 2,
  ERROR: 3,
  FATAL: 4,
  SILENT: 5
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
    return data.replace(/(token|password|key|secret|api_key|apiToken)(["']?\s*[:=]\s*["']?)([^"'\s&,]+)/gi, '$1$2******');
  }

  if (typeof data === 'object' && data !== null) {
    // 递归对象脱敏
    try {
      const masked = Array.isArray(data) ? [] : {};
      for (const key in data) {
        if (Object.prototype.hasOwnProperty.call(data, key)) {
          const lowerKey = key.toLowerCase();
          const isSensitive = lowerKey.includes('token') ||
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

  // 1. 终端渲染 (用于开发调试)
  renderTerminal(level, module, timestamp, traceId, maskedMessage, maskedData);

  // 2. 构造结构化日志对象
  const logEntry = {
    timestamp,
    level,
    traceId,
    module: module || 'core',
    message: maskedMessage,
    data: maskedData
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

function renderTerminal(level, module, timestamp, traceId, message, data) {
  const levelColors = {
    'DEBUG': useColor ? chalk.gray : (t) => t,
    'INFO': useColor ? chalk.blue : (t) => t,
    'WARN': useColor ? chalk.yellow : (t) => t,
    'ERROR': useColor ? chalk.red : (t) => t,
    'FATAL': useColor ? chalk.bgRed.white.bold : (t) => t
  };

  const colorFn = levelColors[level] || ((t) => t);
  const displayTime = formatDisplayTimestamp(timestamp);
  
  // 固定宽度定义
  const COL_TIME = 12;    // HH:mm:ss.SSS
  const COL_LEVEL = 5;    // ERROR
  const COL_MODULE = 12;  // ModuleName
  const COL_TRACE = 8;    // [abc12]

  const timeStr = useColor ? chalk.gray(displayTime.padEnd(COL_TIME)) : displayTime.padEnd(COL_TIME);
  const levelStr = colorFn(level.padEnd(COL_LEVEL));
  
  const rawModule = (module || 'core').substring(0, 10);
  const formattedModule = `[${rawModule}]`.padEnd(12);
  const moduleStr = useColor ? chalk.magenta(formattedModule) : formattedModule;

  const output = `${timeStr} ${levelStr} ${moduleStr} ${message}`;
  console.log(output);

  if (data !== undefined && level !== 'INFO') {
    if (typeof data === 'object') {
      try {
        const json = JSON.stringify(data, null, 2).split('\n').map(line => ' '.repeat(COL_TIME + COL_LEVEL + COL_MODULE + 2) + line).join('\n');
        console.log(colorFn(json));
      } catch (e) {
        console.log(colorFn('   [Complex Data]'));
      }
    } else {
      console.log(colorFn('   ' + data));
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

    start: (message) => {
      const msg = useColor ? chalk.cyan('▶ ' + message) : '▶ ' + message;
      log('INFO', moduleName, msg);
    },

    complete: (message, data) => {
      const msg = useColor ? chalk.green('✓ ' + message) : '✓ ' + message;
      log('INFO', moduleName, msg, data);
    },

    // 恢复这些方法以兼容现有代码，防止报错
    group: (title) => {
      const msg = useColor ? chalk.bold(title) : title;
      log('INFO', moduleName, msg);
    },

    groupItem: (message, data) => {
      const msg = '  • ' + message;
      log('INFO', moduleName, msg, data);
    }
  };
}

const globalLogger = createLogger('');

module.exports = {
  createLogger,
  logger: globalLogger,
  logEmitter,
  getBuffer: () => logBuffer,
  asyncLocalStorage,
  LOG_LEVELS
};

