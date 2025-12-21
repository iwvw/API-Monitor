/**
 * 通用工具函数模块
 */

// 导入新的Toast模块
import toastManager, { toast, showToast as newShowToast } from './toast.js';
import { marked } from 'marked';
import DOMPurify from 'dompurify';

/**
 * 渲染 Markdown 为 HTML (安全模式)
 * 支持多模态数组、JSON 对象及普通字符串
 * @param {any} text - 输入内容
 * @returns {string} 过滤后的 HTML
 */
export function renderMarkdown(text) {
    if (text === undefined || text === null) return '';
    
    let source = '';
    
    // 1. 处理多模态数组 (OpenAI 格式)
    if (Array.isArray(text)) {
        source = text.map(part => {
            if (!part) return '';
            if (typeof part === 'string') return part;
            if (typeof part === 'object') {
                if (part.type === 'text') return part.text || '';
                if (part.type === 'image_url') {
                    const url = part.image_url?.url || '';
                    // 极致去空白处理：HTML 保持在一行，杜绝段落包裹
                    return `<div class="msg-image-container"><a href="javascript:void(0)" class="img-preview-trigger"><img src="${url}" class="msg-inline-image" alt="图片附件" /></a></div>`;
                }
                return `\`${JSON.stringify(part)}\``;
            }
            return String(part);
        }).join(''); // 使用空字符串拼接
    } 
    // 2. 处理单对象
    else if (typeof text === 'object') {
        source = `\`\`\`json\n${JSON.stringify(text, null, 2)}\n\`\`\``;
    }
    // 3. 处理字符串 (防 object object 逃逸)
    else {
        source = String(text);
        if (source === '[object Object]') {
            try { source = '```json\n' + JSON.stringify(text, null, 2) + '\n```'; } catch(e) {}
        }
    }

    try {
        const rawHtml = marked.parse(source, { breaks: true });
        return DOMPurify.sanitize(rawHtml, {
            ADD_ATTR: ['target', 'title', 'rel'],
            ADD_TAGS: ['a', 'img', 'div'],
            // 允许 data: 协议以便查看 Base64 图片
            ALLOWED_URI_REGEXP: /^(?:(?:(?:f|ht)tps?|mailto|tel|callto|cid|xmpp|data):|[^a-z]|[a-z+.\-]+(?:[^a-z+.\-:]|$))/i
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
        hour12: false
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
    const localISOTime = (new Date(d.getTime() - tzoffset)).toISOString().slice(0, -1);
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
            setTimeout(() => inThrottle = false, limit);
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

    const doMask = (str) => {
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
    let regionStr = typeof region === 'object' ? (region.name || region.id || '') : String(region);

    if (!regionStr) return '未知';

    // 如果已经是中文（包含中文字符），直接返回
    if (/[\u4e00-\u9fa5]/.test(regionStr)) {
        return regionStr;
    }

    // 地区名称映射
    const regionMap = {
        'silicon': '硅谷',
        'jakarta': '雅加达',
        'hong kong': '香港',
        'tokyo': '东京',
        'singapore': '新加坡',
        'frankfurt': '法兰克福',
        'london': '伦敦',
        'sydney': '悉尼',
        'taipei': '台北',
        'shanghai': '上海',
        'california': '加州',
        'new jersey': '新泽西',
        'fra': '法兰克福',
        'was': '华盛顿',
        'sin': '新加坡',
        'par': '巴黎',
        'sfo': '金山',
        'nyc': '纽约',
        'tor': '多伦多'
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
