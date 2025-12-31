/**
 * 统一日志管理模块
 * 控制浏览器控制台的日志输出
 */

// 日志级别定义
const LOG_LEVELS = {
  DEBUG: 0,
  INFO: 1,
  WARN: 2,
  ERROR: 3,
  NONE: 4,
};

// 从 localStorage 读取配置，默认只显示警告和错误
const getLogLevel = () => {
  const saved = localStorage.getItem('consoleLogLevel');
  if (saved && LOG_LEVELS[saved] !== undefined) {
    return LOG_LEVELS[saved];
  }
  // 默认级别：WARN（隐藏成功/信息日志）
  return LOG_LEVELS.WARN;
};

// 当前日志级别
let currentLogLevel = getLogLevel();

// 保存原始的 console 方法
const originalConsole = {
  log: console.log.bind(console),
  info: console.info.bind(console),
  warn: console.warn.bind(console),
  error: console.error.bind(console),
  debug: console.debug.bind(console),
};

const isSuccessOrFetchLog = args => {
  try {
    const str = args
      .map(arg => {
        if (typeof arg === 'string') return arg;
        try {
          return JSON.stringify(arg);
        } catch (e) {
          return String(arg);
        }
      })
      .join(' ');
    // 匹配截图中的 Fetch 加载字样以及常见的成功标志
    return /Fetch|已完成加载|✅|SUCCESS|initialized|connected|loading session|restore from cache/i.test(
      str
    );
  } catch (e) {
    return false;
  }
};

/**
 * 覆盖 console 方法，根据日志级别过滤输出
 */
const initLogger = () => {
  // console.log 和 console.info 映射到 INFO 级别
  console.log = (...args) => {
    if (currentLogLevel > LOG_LEVELS.INFO) return;
    // 如果是 WARN 级别，或者是 INFO 级别但包含成功关键字，则拦截
    if (isSuccessOrFetchLog(args) && currentLogLevel >= LOG_LEVELS.INFO) return;

    if (currentLogLevel <= LOG_LEVELS.INFO) {
      originalConsole.log(...args);
    }
  };

  console.info = (...args) => {
    if (currentLogLevel > LOG_LEVELS.INFO) return;
    if (isSuccessOrFetchLog(args) && currentLogLevel >= LOG_LEVELS.INFO) return;

    if (currentLogLevel <= LOG_LEVELS.INFO) {
      originalConsole.info(...args);
    }
  };

  // console.debug 映射到 DEBUG 级别
  console.debug = (...args) => {
    if (currentLogLevel <= LOG_LEVELS.DEBUG) {
      originalConsole.debug(...args);
    }
  };

  // console.warn 保持输出（除非设置为 ERROR 或 NONE）
  console.warn = (...args) => {
    if (currentLogLevel <= LOG_LEVELS.WARN) {
      originalConsole.warn(...args);
    }
  };

  // console.error 始终输出（除非设置为 NONE）
  console.error = (...args) => {
    if (currentLogLevel <= LOG_LEVELS.ERROR) {
      originalConsole.error(...args);
    }
  };
};

/**
 * 设置日志级别
 * @param {'DEBUG' | 'INFO' | 'WARN' | 'ERROR' | 'NONE'} level
 */
const setLogLevel = level => {
  if (LOG_LEVELS[level] !== undefined) {
    currentLogLevel = LOG_LEVELS[level];
    localStorage.setItem('consoleLogLevel', level);
    originalConsole.log(`[Logger] 日志级别已设置为: ${level}`);
  }
};

/**
 * 获取当前日志级别名称
 */
const getLogLevelName = () => {
  return Object.keys(LOG_LEVELS).find(key => LOG_LEVELS[key] === currentLogLevel) || 'UNKNOWN';
};

/**
 * 恢复原始 console 行为
 */
const restoreConsole = () => {
  console.log = originalConsole.log;
  console.info = originalConsole.info;
  console.warn = originalConsole.warn;
  console.error = originalConsole.error;
  console.debug = originalConsole.debug;
};

// 导出原始 console（用于需要强制输出的场景）
export const rawConsole = originalConsole;

// 导出工具函数
export { initLogger, setLogLevel, getLogLevelName, restoreConsole, LOG_LEVELS };

// 自动初始化
initLogger();

// 在全局暴露控制函数（方便在控制台手动调试）
if (typeof window !== 'undefined') {
  window.__setLogLevel = setLogLevel;
  window.__getLogLevel = getLogLevelName;
  window.__restoreConsole = restoreConsole;
}
