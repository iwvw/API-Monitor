/**
 * 通用工具函数模块
 */

// 导入新的Toast模块
import toastManager, { toast, showToast as newShowToast } from './toast.js';

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
 * 格式化日期时间
 * @param {string|Date} date - 日期
 * @param {Object} options - Intl.DateTimeFormat 选项
 * @returns {string} 格式化后的日期时间
 */
export function formatDateTime(date, options = null) {
    if (!date) return '-';
    const d = new Date(date);
    const defaultOptions = options || {
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
        hour12: false
    };
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
