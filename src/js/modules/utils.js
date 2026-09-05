/**
 * 通用工具函数模块
 */

// 导入新的Toast模块
import toastManager, { toast, showToast as newShowToast } from './toast.js';
// 注意：renderMarkdown 已拆分到 markdown.js（含 marked/DOMPurify/katex 重依赖），
// 请直接从 './markdown.js' 导入，勿在此 re-export，否则重依赖会回流到本模块。

/**
 * 显示 Toast 提示
 * @param {string} message - 提示消息
 * @param {string} type - 提示类型 (success, error, warning, info)
 */
export function showToast(message, type = 'info') {
  return newShowToast(message, type);
}

// 导出新的toast API供高级使用
export { toastManager, toast };

/**
 * HTML 转义
 * @param {string} text - 要转义的文本
 * @returns {string} 转义后的文本
 */
export function escapeHtml(text) {
  const div = document.createElement('div');
  div.textContent = text;
  return div.innerHTML;
}

/**
 * 全局展示时区（'system' 表示跟随浏览器本地时区）。
 * 由 store 在用户设置加载/保存后同步；公开页等未加载 store 的场景保持 'system'。
 */
let displayTimeZone = 'system';

export const setDisplayTimeZone = (zone) => {
  displayTimeZone = typeof zone === 'string' && zone.trim() ? zone.trim() : 'system';
};

export const getDisplayTimeZone = () => displayTimeZone;

/**
 * 格式化日期时间 (默认转换全局展示时区，'system' 时用浏览器本地时区)
 * @param {string|Date|number} date - 日期
 * @param {Object} options - Intl.DateTimeFormat 选项（显式传入的 timeZone 优先）
 * @returns {string} 格式化后的日期时间
 */
export function formatDateTime(date, options = null) {
  if (!date) return '-';

  let d;
  if (typeof date === 'string') {
    // 如果是 SQLite 的 YYYY-MM-DD HH:mm:ss 格式且没有时区，补全 Z 使其按 UTC 解析
    if (/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/.test(date)) {
      d = new Date(date.replace(' ', 'T') + 'Z');
    } else if (!date.includes('Z') && !date.includes('+') && !date.includes('-')) {
      // 其他没有时区信息的字符串，假设是 UTC
      d = new Date(date + 'Z');
    } else {
      d = new Date(date);
    }
  } else {
    d = new Date(date);
  }

  // 如果解析失败，回退到原始解析
  if (isNaN(d.getTime())) {
    d = new Date(date);
  }

  const defaultOptions = {
    ...(options || {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
      hour12: false,
    }),
  };

  if (!defaultOptions.timeZone && displayTimeZone && displayTimeZone !== 'system') {
    defaultOptions.timeZone = displayTimeZone;
  }

  // toLocaleString 会自动使用浏览器当前时区（或 defaultOptions.timeZone）
  return d.toLocaleString('zh-CN', defaultOptions);
}

/**
 * 获取本地时间戳字符串 (用于文件名)
 * 格式: YYYY-MM-DD_HH-MM-SS
 * @returns {string}
 */
export function getLocalTimestamp() {
  const now = new Date();
  const year = now.getFullYear();
  const month = String(now.getMonth() + 1).padStart(2, '0');
  const day = String(now.getDate()).padStart(2, '0');
  const hours = String(now.getHours()).padStart(2, '0');
  const minutes = String(now.getMinutes()).padStart(2, '0');
  const seconds = String(now.getSeconds()).padStart(2, '0');
  return `${year}-${month}-${day}_${hours}-${minutes}-${seconds}`;
}

/**
 * 将日期转换为本地 ISO 格式 (YYYY-MM-DDTHH:mm:ss.sss)
 * @param {Date|string} date
 * @returns {string}
 */
export function formatLocalISO(date) {
  const d = date ? new Date(date) : new Date();
  const tzoffset = d.getTimezoneOffset() * 60000;
  const localISOTime = new Date(d.getTime() - tzoffset).toISOString().slice(0, -1);
  return localISOTime;
}

/**
 * 格式化文件大小
 * @param {number} bytes - 字节数
 * @returns {string} 格式化后的大小
 */
export function formatFileSize(bytes) {
  if (!Number.isFinite(bytes) || bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.min(Math.floor(Math.log(Math.abs(bytes)) / Math.log(k)), sizes.length - 1);
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
}

/**
 * 格式化 Token 数量 (K/M/B)
 * @param {number} num - Token 数量
 * @returns {string} 格式化后的字符串
 */
export function formatTokens(num) {
  if (!num || num === 0) return '0';
  if (num < 1000) return num.toString();
  
  if (num < 1000000) {
    return (num / 1000).toFixed(1).replace(/\.0$/, '') + 'K';
  }
  
  if (num < 1000000000) {
    return (num / 1000000).toFixed(1).replace(/\.0$/, '') + 'M';
  }
  
  return (num / 1000000000).toFixed(2).replace(/\.00$/, '') + 'B';
}

/**
 * 格式化运行时间 (增强版)
 * 支持 "up 1 day, 10:23", "12345" (秒), "2 days 3 hours" 等格式
 * @param {string|number} uptimeStr - 运行时间字符串或秒数
 * @returns {string} 中文格式时间 (e.g. "1天 10时 23分")
 */
export function formatUptime(uptimeStr) {
  if (uptimeStr === undefined || uptimeStr === null) return '-';

  // 处理数字输入 (视为秒)
  if (typeof uptimeStr === 'number') {
    const days = Math.floor(uptimeStr / 86400);
    const hours = Math.floor((uptimeStr % 86400) / 3600);
    const minutes = Math.floor((uptimeStr % 3600) / 60);

    let result = '';
    if (days > 0) result += `${days}天`;
    if (hours > 0) result += `${hours}时`;
    if (minutes > 0) result += `${minutes}分`;
    return result || '0分';
  }

  if (typeof uptimeStr !== 'string') return uptimeStr;

  // 移除 "up " 前缀
  const str = uptimeStr.replace(/^up\s+/i, '');

  let days = 0;
  let hours = 0;
  let minutes = 0;

  // 尝试匹配 "1 day, 10:23" 或 "10:23" 格式 (Linux uptime 常见)
  const timeMatch = str.match(/(?:(\d+)\s*days?,\s*)?(\d{1,2}):(\d{2})/i);

  if (timeMatch) {
    if (timeMatch[1]) days = parseInt(timeMatch[1], 10);
    hours = parseInt(timeMatch[2], 10);
    minutes = parseInt(timeMatch[3], 10);
  } else {
    // 尝试匹配 "1 week, 2 days" 或 "1w, 2d" 格式
    const weekMatch = str.match(/(\d+)\s*(weeks?|w)/i);
    const dayMatch = str.match(/(\d+)\s*(days?|d)/i);
    const hourMatch = str.match(/(\d+)\s*(hours?|h)/i);
    const minMatch = str.match(/(\d+)\s*(minutes?|m)/i);

    if (dayMatch) days = parseInt(dayMatch[1], 10);
    if (weekMatch) days += parseInt(weekMatch[1], 10) * 7;
    if (hourMatch) hours = parseInt(hourMatch[1], 10);
    if (minMatch) minutes = parseInt(minMatch[1], 10);
  }

  // 构建中文格式 (紧凑)
  let result = '';
  if (days > 0) result += `${days}天`;
  if (hours > 0) result += `${hours}时`;
  if (minutes > 0) result += `${minutes}分`;

  // 如果都是0，但有一个 parsing 发生，显示 "0分"
  // 如果没有任何匹配，返回原字符串 (可能是其他格式)
  if (result === '') {
    if (str.includes('min') || str.includes('sec')) return '刚刚';
    return uptimeStr; // 原样返回，防止显示错误
  }

  return result;
}

/**
 * 防抖函数
 * @param {Function} func - 要防抖的函数
 * @param {number} wait - 等待时间（毫秒）
 * @returns {Function} 防抖后的函数
 */
export function debounce(func, wait) {
  let timeout;
  return function executedFunction(...args) {
    const later = () => {
      clearTimeout(timeout);
      func(...args);
    };
    clearTimeout(timeout);
    timeout = setTimeout(later, wait);
  };
}

/**
 * 节流函数
 * @param {Function} func - 要节流的函数
 * @param {number} limit - 时间限制（毫秒）
 * @returns {Function} 节流后的函数
 */
export function throttle(func, limit) {
  let inThrottle;
  return function (...args) {
    if (!inThrottle) {
      func.apply(this, args);
      inThrottle = true;
      setTimeout(() => (inThrottle = false), limit);
    }
  };
}

/**
 * 深拷贝对象
 * @param {*} obj - 要拷贝的对象
 * @returns {*} 拷贝后的对象
 */
export function deepClone(obj) {
  if (obj === null || typeof obj !== 'object') return obj;
  if (obj instanceof Date) return new Date(obj.getTime());
  if (obj instanceof Array) return obj.map(item => deepClone(item));
  if (obj instanceof Object) {
    const clonedObj = {};
    for (const key in obj) {
      if (obj.hasOwnProperty(key)) {
        clonedObj[key] = deepClone(obj[key]);
      }
    }
    return clonedObj;
  }
}

/**
 * 格式化地址（支持打码/隐藏）
 * @param {string} address - 要格式化的地址 (IP 或 域名)
 * @param {string} mode - 显示模式 ('normal', 'masked', 'hidden')
 * @returns {string} 格式化后的地址
 */
export function maskAddress(address, mode = 'normal') {
  if (!address) return '';
  if (mode === 'normal') return address;
  if (mode === 'hidden') return '****';

  // 处理带有协议和路径的 URL (API Endpoint 常见)
  let displayAddress = address;
  let prefix = '';
  let suffix = '';

  try {
    if (address.includes('://')) {
      const url = new URL(address);
      prefix = url.protocol + '//';
      displayAddress = url.hostname;
      suffix = url.pathname !== '/' ? url.pathname : '';
      // 端口不泄露具体值，但保留存在性提示（host:8443 与默认端口可区分）
      if (url.port && url.port !== '443' && url.port !== '80') {
        suffix = ':' + '****' + suffix;
      }
    }
  } catch (e) {
    // 如果不是标准 URL，则按原样处理
  }

  const doMask = str => {
    // 严谨检测 IPv4
    const ipv4Regex = /^(\d{1,3}\.){3}\d{1,3}$/;
    if (ipv4Regex.test(str)) {
      const parts = str.split('.');
      return `${parts[0]}.${parts[1]}.*.*`;
    }

    // 域名或其他: example.com -> ex****.com
    const parts = str.split('.');
    if (parts.length >= 2) {
      const main = parts[0];
      const tld = parts[parts.length - 1];
      if (main.length > 2) {
        return main.substring(0, 2) + '****.' + tld;
      }
    }
    return str.length > 4 ? str.substring(0, 2) + '****' : '****';
  };

  return prefix + doMask(displayAddress) + suffix;
}
/**
 * 格式化地区名称（支持多种平台数据结构）
 * @param {string|Object} region - 地区字符串或包含 name 的对象
 * @returns {string} 格式化后的中文名称
 */
export function formatRegion(region) {
  if (!region) return '未知';

  // 兼容对象格式 (PaaS) 和字符串格式 (Koyeb)
  const regionStr = typeof region === 'object' ? region.name || region.id || '' : String(region);

  if (!regionStr) return '未知';

  // 如果已经是中文（包含中文字符），直接返回
  if (/[\u4e00-\u9fa5]/.test(regionStr)) {
    return regionStr;
  }

  // 地区名称映射
  const regionMap = {
    silicon: '硅谷',
    jakarta: '雅加达',
    'hong kong': '香港',
    tokyo: '东京',
    singapore: '新加坡',
    frankfurt: '法兰克福',
    london: '伦敦',
    sydney: '悉尼',
    taipei: '台北',
    shanghai: '上海',
    california: '加州',
    'new jersey': '新泽西',
    fra: '法兰克福',
    was: '华盛顿',
    sin: '新加坡',
    par: '巴黎',
    sfo: '金山',
    nyc: '纽约',
    tor: '多伦多',
    // 阿里云/腾讯云常见区域适配
    'cn-hangzhou': '杭州',
    'cn-shanghai': '上海',
    'cn-beijing': '北京',
    'cn-guangzhou': '广州',
    'cn-shenzhen': '深圳',
    'cn-hongkong': '香港',
    'ap-guangzhou': '广州',
    'ap-shanghai': '上海',
    'ap-beijing': '北京',
    'ap-hongkong': '香港',
    'ap-singapore': '新加坡',
    'ap-nanjing': '南京',
    'ap-chengdu': '成都',
  };

  // 模糊匹配逻辑
  const lowerRegion = regionStr.toLowerCase();
  for (const [key, value] of Object.entries(regionMap)) {
    if (lowerRegion.includes(key)) {
      return value;
    }
  }

  return regionStr;
}

/**
 * 格式化网速为紧凑格式
 * 例如: "1.5 MB/s" -> "1.5M", "10 KB/s" -> "10K", "0 B/s" -> "0B"
 */
export function formatSpeedCompact(speed) {
  if (!speed) return '0B';
  // 移除 "/s" 后缀，移除空格，保留数字和单位字母
  return speed
    .replace(/\/s$/i, '') // 移除 /s
    .replace(/\s+/g, '') // 移除空格
    .replace(/(\d+\.?\d*)([KMGT]?)B?/i, '$1$2'); // 简化单位
}

/**
 * 解析网速为数字和单位分离的对象
 * 例如: "1.5 MB/s" -> { num: "1.5", unit: "M" }
 */
export function parseSpeed(speed) {
  if (!speed) return { num: '0', unit: 'B' };
  const cleaned = speed.replace(/\/s$/i, '').replace(/\s+/g, '');
  const match = cleaned.match(/^(\d+\.?\d*)([KMGT]?)B?$/i);
  if (match) {
    return { num: match[1], unit: match[2] ? match[2].toUpperCase() : 'B' };
  }
  return { num: '0', unit: 'B' };
}
