/**
 * 统一日志工具模块
 * 提供规范化的日志输出，支持不同级别和模块分类
 */

const chalk = require('chalk');

// 日志级别
const LOG_LEVELS = {
  DEBUG: 0,
  INFO: 1,
  WARN: 2,
  ERROR: 3,
  SILENT: 4
};

// 当前日志级别（从环境变量读取，默认为INFO）
const currentLevel = LOG_LEVELS[process.env.LOG_LEVEL?.toUpperCase()] ?? LOG_LEVELS.INFO;

// 是否启用彩色输出
const useColor = process.env.NO_COLOR !== '1';

// 格式化时间戳
function getTimestamp() {
  const now = new Date();
  const hours = String(now.getHours()).padStart(2, '0');
  const minutes = String(now.getMinutes()).padStart(2, '0');
  const seconds = String(now.getSeconds()).padStart(2, '0');
  const ms = String(now.getMilliseconds()).padStart(3, '0');
  return `${hours}:${minutes}:${seconds}.${ms}`;
}

// 格式化模块名称
function formatModule(module) {
  return module ? `[${module}]` : '';
}

// 日志输出核心函数
function log(level, module, message, data) {
  if (LOG_LEVELS[level] < currentLevel) return;

  const timestamp = getTimestamp();
  const moduleStr = formatModule(module);

  let prefix = '';
  let colorFn = (text) => text;

  switch (level) {
    case 'DEBUG':
      prefix = '🔍';
      colorFn = useColor ? chalk.gray : (text) => text;
      break;
    case 'INFO':
      prefix = 'ℹ️ ';
      colorFn = useColor ? chalk.cyan : (text) => text;
      break;
    case 'WARN':
      prefix = '⚠️ ';
      colorFn = useColor ? chalk.yellow : (text) => text;
      break;
    case 'ERROR':
      prefix = '❌';
      colorFn = useColor ? chalk.red : (text) => text;
      break;
  }

  const timestampStr = useColor ? chalk.gray(timestamp) : timestamp;
  const moduleColor = useColor ? chalk.blue : (text) => text;
  const output = `${timestampStr} ${prefix} ${moduleColor(moduleStr)} ${message}`;

  console.log(colorFn(output));

  // 如果有额外数据，格式化输出
  if (data !== undefined) {
    if (typeof data === 'object') {
      console.log(colorFn('   ' + JSON.stringify(data, null, 2).split('\n').join('\n   ')));
    } else {
      console.log(colorFn('   ' + data));
    }
  }
}

// 创建模块日志器
function createLogger(moduleName) {
  return {
    debug: (message, data) => log('DEBUG', moduleName, message, data),
    info: (message, data) => log('INFO', moduleName, message, data),
    warn: (message, data) => log('WARN', moduleName, message, data),
    error: (message, data) => log('ERROR', moduleName, message, data),

    // 便捷方法
    success: (message, data) => {
      const successMsg = useColor ? chalk.green('✓ ' + message) : '✓ ' + message;
      log('INFO', moduleName, successMsg, data);
    },

    start: (message) => {
      const startMsg = useColor ? chalk.cyan('▶ ' + message) : '▶ ' + message;
      log('INFO', moduleName, startMsg);
    },

    complete: (message, data) => {
      const completeMsg = useColor ? chalk.green('✓ ' + message) : '✓ ' + message;
      log('INFO', moduleName, completeMsg, data);
    },

    // 分组日志
    group: (title) => {
      const groupMsg = useColor ? chalk.bold(title) : title;
      log('INFO', moduleName, groupMsg);
    },

    groupItem: (message, data) => {
      const itemMsg = '  • ' + message;
      log('INFO', moduleName, itemMsg, data);
    }
  };
}

// 全局日志器（无模块名）
const globalLogger = createLogger('');

module.exports = {
  createLogger,
  logger: globalLogger,
  LOG_LEVELS
};
