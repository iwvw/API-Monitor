/**
 * 通用工具函数模块
 */

// 导入新的Toast模块
import toastManager, { toast, showToast as newShowToast } from './toast.js';
import { marked } from 'marked';
import DOMPurify from 'dompurify';
import katex from 'katex';
import 'katex/dist/katex.min.css';

/**
 * 渲染 Markdown 为 HTML (安全模式)
 * ... (existing renderMarkdown implementation)
 */
export function renderMarkdown(text) {
  if (text === undefined || text === null) return '';

  let source = '';

  // 1. 处理多模态数组 (OpenAI 格式)
  if (Array.isArray(text)) {
    source = text
      .map(part => {
        if (!part) return '';
        if (typeof part === 'string') return part;
        if (typeof part === 'object') {
          // 支持带 thought 属性的文本块 (Gemini/DeepSeek 后端适配)
          if (part.type === 'text' || !part.type) {
            const content = part.text || part.content || '';
            if (part.thought) {
              return `<think>${content}</think>`;
            }
            return content;
          }
          if (part.type === 'image_url') {
            const url = part.image_url?.url || '';
            // 极致去空白处理：HTML 保持在一行，杜绝段落包裹
            return `<div class="msg-image-container"><a href="javascript:void(0)" class="img-preview-trigger"><img src="${url}" class="msg-inline-image" alt="图片附件" /></a></div>`;
          }
          return `\`${JSON.stringify(part)}\``;
        }
        return String(part);
      })
      .join(''); // 使用空字符串拼接
  }
  // 2. 处理单对象
  else if (typeof text === 'object') {
    source = `\`\`\`json\n${JSON.stringify(text, null, 2)}\n\`\`\``;
  }
  // 3. 处理字符串 (防 object object 逃逸)
  else {
    source = String(text);
    if (source === '[object Object]') {
      try {
        source = '```json\n' + JSON.stringify(text, null, 2) + '\n```';
      } catch (e) { }
    }
  }

  // 4. 预处理数学公式 (LaTeX) - 使用占位符保护公式不被 marked 破坏
  const mathBlocks = [];
  const addMath = (content, displayMode) => {
    try {
      const html = katex.renderToString(content.trim(), { displayMode, throwOnError: false });
      mathBlocks.push(displayMode ? `<div class="math-block">${html}</div>` : `<span class="math-inline">${html}</span>`);
      return `@@MATH_${mathBlocks.length - 1}@@`;
    } catch (e) {
      return content;
    }
  };

  // 4.1 块级公式 (优先处理)
  source = source.replace(/\$\$([\s\S]+?)\$\$/g, (m, c) => addMath(c, true));
  source = source.replace(/\\\[([\s\S]+?)\\\]/g, (m, c) => addMath(c, true));

  // 4.2 行内公式
  source = source.replace(/\\\(([\s\S]+?)\\\)/g, (m, c) => addMath(c, false));
  source = source.replace(/\$([^\s$][^$]*?[^\s$])\$/g, (m, c) => addMath(c, false));
  source = source.replace(/\$([^\s$])\$/g, (m, c) => addMath(c, false));

  // 5. 预处理思考标签 <think> (DeepSeek/Gemini)
  source = source.replace(/<think>([\s\S]*?)<\/think>/gi, (match, content) => {
    return `<details class="reasoning-details"><summary><i class="fas fa-brain" style="margin-right: 6px;"></i>思考过程</summary><div class="reasoning-content-inner">\n\n${content}\n\n</div></details>`;
  });

  try {
    // 渲染 Markdown
    let rawHtml = marked.parse(source, { breaks: true, gfm: true });

    // 6. 还原数学公式
    mathBlocks.forEach((html, index) => {
      rawHtml = rawHtml.replace(`@@MATH_${index}@@`, html);
    });

    return DOMPurify.sanitize(rawHtml, {
      ADD_ATTR: ['target', 'title', 'rel', 'open', 'class', 'style', 'aria-hidden', 'viewBox', 'd', 'fill'],
      ADD_TAGS: ['a', 'img', 'div', 'details', 'summary', 'i', 'span', 'svg', 'path', 'math', 'semantics', 'mrow', 'annotation', 'mstyle', 'mo', 'mi', 'mn', 'msup', 'msub', 'mfrac', 'msqrt', 'root', 'mtd', 'mtr', 'mtable'],
      // 允许 data: 协议以便查看 Base64 图片
      ALLOWED_URI_REGEXP:
        /^(?:(?:(?:f|ht)tps?|mailto|tel|callto|cid|xmpp|data):|[^a-z]|[a-z+.\-]+(?:[^a-z+.\-:]|$))/i,
    });
  } catch (e) {
    console.error('Markdown 解析失败:', e);
    return source;
  }
}

/**
 * 显示 Toast 提示
 * @param {string} message - 提示消息
 * @param {string} type - 提示类型 (success, error, warning, info)
 */
export function showToast(message, type = 'info') {
  // 优先使用 Vue 的全局 Toast 系统
  if (window.vueApp && window.vueApp.showGlobalToast) {
    window.vueApp.showGlobalToast(message, type);
    return;
  }

  // 使用新的Toast系统
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
 * 格式化日期时间 (自动转换浏览器本地时区)
 * @param {string|Date|number} date - 日期
 * @param {Object} options - Intl.DateTimeFormat 选项
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

  const defaultOptions = options || {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  };

  // toLocaleString 会自动使用浏览器当前时区
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
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
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
      if (url.port) prefix += ''; // 端口通常也打码，所以合并到 hostname 处理
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

  // 兼容对象格式 (Zeabur) 和字符串格式 (Koyeb)
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
