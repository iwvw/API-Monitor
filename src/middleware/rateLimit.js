/**
 * 速率限制中间件
 * 防止 API 被滥用
 */

const rateLimit = require('express-rate-limit');
const { createLogger } = require('../utils/logger');

const logger = createLogger('RateLimit');

/**
 * 创建速率限制器
 * @param {Object} options - 配置选项
 * @returns {Function} Express 中间件
 */
function createLimiter(options = {}) {
  const defaultOptions = {
    windowMs: 15 * 60 * 1000, // 15 分钟
    max: 100, // 每个窗口最多 100 个请求
    standardHeaders: true, // 返回标准限制头 `RateLimit-*`
    legacyHeaders: false, // 禁用 `X-RateLimit-*` 头
    message: {
      success: false,
      error: {
        code: 'RATE_LIMIT_EXCEEDED',
        message: '请求过于频繁，请稍后再试',
      },
    },
    handler: (req, res, next, options) => {
      logger.warn(`Rate limit exceeded: ${req.ip} - ${req.method} ${req.path}`);
      res.status(options.statusCode).json(options.message);
    },
    skip: req => {
      // 跳过健康检查端点
      return req.path === '/health' || req.path === '/api/health';
    },
    keyGenerator: req => {
      // 使用标准 IP 逻辑，并附加 User-Agent 以增加区分度
      return req.ip + (req.headers['user-agent'] || '');
    },
    validate: {
      xForwardedForHeader: false,
      keyGeneratorIpFallback: false,
    },
  };

  return rateLimit({ ...defaultOptions, ...options });
}

/**
 * 通用 API 限制器 - 为了开发环境友好，显著放宽上限
 * 1000 请求 / 15 分钟
 */
const generalLimiter = createLimiter({
  windowMs: 15 * 60 * 1000,
  max: process.env.NODE_ENV === 'development' ? 5000 : 1000,
});

/**
 * 认证端点限制器 - 严格
 * 生产环境: 5 请求 / 15 分钟 (防止暴力破解)
 * 开发环境: 100 请求 / 15 分钟 (方便调试)
 */
const authLimiter = createLimiter({
  windowMs: 15 * 60 * 1000,
  max: process.env.NODE_ENV === 'development' ? 100 : 5,
  message: {
    success: false,
    error: {
      code: 'AUTH_RATE_LIMIT',
      message: '登录尝试过于频繁，请 15 分钟后再试',
    },
  },
});

/**
 * 登录成功后重置限制
 */
const loginSuccessLimiter = createLimiter({
  windowMs: 60 * 60 * 1000, // 1 小时
  max: 10,
  skipSuccessfulRequests: true, // 成功的请求不计入
});

/**
 * API 代理限制器 - 中等
 * 30 请求 / 分钟 (OpenAI, Gemini 等)
 */
const proxyLimiter = createLimiter({
  windowMs: 60 * 1000, // 1 分钟
  max: 30,
  message: {
    success: false,
    error: {
      code: 'PROXY_RATE_LIMIT',
      message: 'API 代理请求过于频繁',
    },
  },
});

/**
 * 音频代理限制器 - 宽松
 * 100 请求 / 分钟 (音频流需要多次请求)
 */
const audioProxyLimiter = createLimiter({
  windowMs: 60 * 1000,
  max: 100,
});

/**
 * 文件上传限制器 - 严格
 * 10 请求 / 小时
 */
const uploadLimiter = createLimiter({
  windowMs: 60 * 60 * 1000, // 1 小时
  max: 10,
  message: {
    success: false,
    error: {
      code: 'UPLOAD_RATE_LIMIT',
      message: '文件上传过于频繁',
    },
  },
});

/**
 * SSH 连接限制器 - 中等
 * 20 连接 / 10 分钟
 */
const sshLimiter = createLimiter({
  windowMs: 10 * 60 * 1000,
  max: 20,
  message: {
    success: false,
    error: {
      code: 'SSH_RATE_LIMIT',
      message: 'SSH 连接请求过于频繁',
    },
  },
});

module.exports = {
  createLimiter,
  generalLimiter,
  authLimiter,
  loginSuccessLimiter,
  proxyLimiter,
  audioProxyLimiter,
  uploadLimiter,
  sshLimiter,
};
