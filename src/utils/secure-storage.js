/**
 * 敏感数据存储辅助模块
 * 提供透明的加密/解密层
 */

const { encrypt, decrypt } = require('./encryption');
const { createLogger } = require('./logger');

const logger = createLogger('SecureStorage');

/**
 * 判断字符串是否是加密格式 (iv:authTag:data)
 * @param {string} value - 要检查的值
 * @returns {boolean}
 */
function isEncrypted(value) {
    if (!value || typeof value !== 'string') return false;
    const parts = value.split(':');
    return parts.length === 3 && parts.every((p) => /^[0-9a-f]+$/i.test(p));
}

/**
 * 安全加密 - 如果已加密则不重复加密
 * @param {string} value - 要加密的值
 * @returns {string} 加密后的值
 */
function secureEncrypt(value) {
    if (!value) return value;
    if (isEncrypted(value)) {
        // 已经是加密格式，不重复加密
        return value;
    }
    try {
        return encrypt(value);
    } catch (error) {
        logger.error('加密失败:', error.message);
        throw new Error('数据加密失败');
    }
}

/**
 * 安全解密 - 如果不是加密格式则原样返回
 * @param {string} value - 要解密的值
 * @returns {string} 解密后的值
 */
function secureDecrypt(value) {
    if (!value) return value;
    if (!isEncrypted(value)) {
        // 不是加密格式，可能是明文（兼容旧数据）
        return value;
    }
    try {
        return decrypt(value);
    } catch (error) {
        // 解密失败，可能是损坏的数据或格式类似但不是加密的
        logger.warn('解密失败，返回原值:', error.message);
        return value;
    }
}

/**
 * 批量加密对象中的指定字段
 * @param {Object} obj - 要处理的对象
 * @param {string[]} fields - 要加密的字段名
 * @returns {Object} 处理后的对象副本
 */
function encryptFields(obj, fields) {
    if (!obj || typeof obj !== 'object') return obj;

    const result = { ...obj };
    for (const field of fields) {
        if (result[field]) {
            result[field] = secureEncrypt(result[field]);
        }
    }
    return result;
}

/**
 * 批量解密对象中的指定字段
 * @param {Object} obj - 要处理的对象
 * @param {string[]} fields - 要解密的字段名
 * @returns {Object} 处理后的对象副本
 */
function decryptFields(obj, fields) {
    if (!obj || typeof obj !== 'object') return obj;

    const result = { ...obj };
    for (const field of fields) {
        if (result[field]) {
            result[field] = secureDecrypt(result[field]);
        }
    }
    return result;
}

/**
 * 敏感字段配置
 */
const SENSITIVE_FIELDS = {
    // 服务器账号
    server: ['password', 'private_key', 'passphrase'],

    // API 账号
    account: ['api_token', 'api_key', 'secret_key', 'access_token', 'refresh_token'],

    // 音乐模块
    music: ['cookie'],

    // 通用
    common: ['password', 'token', 'secret', 'key', 'cookie'],
};

/**
 * 创建安全存储包装器
 * @param {string} category - 数据类别
 * @returns {Object} 包含 encrypt/decrypt 方法的对象
 */
function createSecureWrapper(category) {
    const fields = SENSITIVE_FIELDS[category] || SENSITIVE_FIELDS.common;

    return {
        /**
         * 加密对象中的敏感字段
         */
        encrypt(obj) {
            return encryptFields(obj, fields);
        },

        /**
         * 解密对象中的敏感字段
         */
        decrypt(obj) {
            return decryptFields(obj, fields);
        },

        /**
         * 加密数组中所有对象的敏感字段
         */
        encryptMany(arr) {
            if (!Array.isArray(arr)) return arr;
            return arr.map((item) => encryptFields(item, fields));
        },

        /**
         * 解密数组中所有对象的敏感字段
         */
        decryptMany(arr) {
            if (!Array.isArray(arr)) return arr;
            return arr.map((item) => decryptFields(item, fields));
        },
    };
}

// 预置的安全包装器
const serverSecure = createSecureWrapper('server');
const accountSecure = createSecureWrapper('account');
const musicSecure = createSecureWrapper('music');

/**
 * 遮蔽敏感数据（用于日志）
 * @param {string} value - 原始值
 * @param {number} showFirst - 显示前几位
 * @param {number} showLast - 显示后几位
 * @returns {string} 遮蔽后的值
 */
function maskSensitive(value, showFirst = 4, showLast = 4) {
    if (!value || typeof value !== 'string') return '***';
    if (value.length <= showFirst + showLast) {
        return '*'.repeat(value.length);
    }
    const first = value.substring(0, showFirst);
    const last = value.substring(value.length - showLast);
    const middle = '*'.repeat(Math.min(value.length - showFirst - showLast, 8));
    return `${first}${middle}${last}`;
}

/**
 * 从对象中移除敏感字段（用于响应）
 * @param {Object} obj - 原始对象
 * @param {string[]} fields - 要移除的字段
 * @returns {Object} 处理后的对象
 */
function removeSensitiveFields(obj, fields = SENSITIVE_FIELDS.common) {
    if (!obj || typeof obj !== 'object') return obj;

    const result = { ...obj };
    for (const field of fields) {
        if (field in result) {
            delete result[field];
        }
    }
    return result;
}

/**
 * 遮蔽对象中的敏感字段（用于日志）
 * @param {Object} obj - 原始对象
 * @param {string[]} fields - 要遮蔽的字段
 * @returns {Object} 处理后的对象
 */
function maskSensitiveFields(obj, fields = SENSITIVE_FIELDS.common) {
    if (!obj || typeof obj !== 'object') return obj;

    const result = { ...obj };
    for (const field of fields) {
        if (result[field]) {
            result[field] = maskSensitive(result[field]);
        }
    }
    return result;
}

module.exports = {
    // 基础函数
    isEncrypted,
    secureEncrypt,
    secureDecrypt,
    encryptFields,
    decryptFields,

    // 配置
    SENSITIVE_FIELDS,

    // 工厂函数
    createSecureWrapper,

    // 预置包装器
    serverSecure,
    accountSecure,
    musicSecure,

    // 辅助函数
    maskSensitive,
    removeSensitiveFields,
    maskSensitiveFields,
};
