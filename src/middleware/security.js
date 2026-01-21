/**
 * 安全中间件配置
 * 使用 Helmet 设置 HTTP 安全头
 */

const helmet = require('helmet');

/**
 * 配置 Helmet 安全头
 * @param {Object} options - 自定义选项
 * @returns {Function} Helmet 中间件
 */
function configureHelmet(options = {}) {
    const isDev = process.env.NODE_ENV !== 'production';

    return helmet({
        // 内容安全策略 - 放宽以支持更多 CDN 和外部资源
        contentSecurityPolicy: isDev
            ? false // 开发环境禁用 CSP (方便调试)
            : {
                directives: {
                    defaultSrc: ["'self'"],
                    scriptSrc: [
                        "'self'",
                        "'unsafe-inline'", // Vue 需要
                        "'unsafe-eval'", // Vue 开发模式需要
                        'https://cdn.jsdelivr.net',
                        'https://unpkg.com',
                        'https://cdnjs.cloudflare.com',
                        'https://*.bytecdntp.com', // 字节跳动 CDN
                        'https://lf3-cdn.bytecdntp.com',
                        'https://cdn.bootcdn.net', // BootCDN
                        'https://cdn.staticfile.org', // Staticfile CDN
                    ],
                    styleSrc: [
                        "'self'",
                        "'unsafe-inline'",
                        'https:', // 允许所有 HTTPS 样式
                    ],
                    fontSrc: [
                        "'self'",
                        'https:', // 允许所有 HTTPS 字体
                        'data:',
                    ],
                    imgSrc: ["'self'", 'data:', 'https:', 'blob:', 'http:'],
                    mediaSrc: ["'self'", 'https:', 'blob:', 'http:'],
                    connectSrc: [
                        "'self'",
                        'wss:',
                        'ws:',
                        'https:',
                        'http:', // 允许所有 HTTP/HTTPS 连接
                        'data:', // 允许 data URL（图片上传使用）
                        'blob:', // 允许 blob URL
                    ],
                    objectSrc: ["'none'"],
                    frameAncestors: ["'self'"],
                    formAction: ["'self'"],
                    upgradeInsecureRequests: null, // 显式禁用：防止浏览器将请求强制升级为 HTTPS
                },
            },

        // 跨域嵌入保护
        crossOriginEmbedderPolicy: false, // 某些 CDN 资源需要关闭

        // 跨域打开者策略
        // 使用 unsafe-none 避免在 HTTP 环境下产生警告
        crossOriginOpenerPolicy: false,

        // 跨域资源策略
        crossOriginResourcePolicy: { policy: 'cross-origin' },

        // DNS 预取控制
        dnsPrefetchControl: { allow: true },

        // 期望 CT (Certificate Transparency)
        // expectCt: false, // 已弃用

        // 框架选项 - 防止点击劫持
        frameguard: { action: 'sameorigin' },

        // 隐藏 X-Powered-By
        hidePoweredBy: true,

        // HSTS (仅在配置 HTTPS 时启用)
        // 使用 maxAge: 0 强制清除浏览器可能缓存的 HSTS 策略
        hsts: { maxAge: 0 },

        // IE 无嗅探
        ieNoOpen: true,

        // 禁用 MIME 类型嗅探
        noSniff: true,

        // 来源策略集群 - 关闭以避免在 HTTP 环境下产生 Origin-Agent-Cluster 警告
        originAgentCluster: false,

        // 权限策略
        permittedCrossDomainPolicies: { permittedPolicies: 'none' },

        // Referrer 策略
        referrerPolicy: { policy: 'strict-origin-when-cross-origin' },

        // XSS 过滤器 (现代浏览器已内置，但仍建议设置)
        xssFilter: true,
    });
}

/**
 * API 专用安全头 (更宽松)
 */
function apiSecurityHeaders(req, res, next) {
    // API 响应不需要某些浏览器安全策略
    res.setHeader('X-Content-Type-Options', 'nosniff');
    res.setHeader('X-Frame-Options', 'DENY');
    res.setHeader('Cache-Control', 'no-store, no-cache, must-revalidate, private');
    res.setHeader('Pragma', 'no-cache');
    res.setHeader('Expires', '0');

    next();
}

/**
 * CORS 配置
 * @param {Object} options - 自定义选项
 */
function corsConfig(options = {}) {
    const allowedOrigins = options.origins || ['*'];
    const isDev = process.env.NODE_ENV !== 'production';

    return {
        origin: isDev
            ? true // 开发环境允许所有来源
            : (origin, callback) => {
                if (!origin || allowedOrigins.includes('*') || allowedOrigins.includes(origin)) {
                    callback(null, true);
                } else {
                    callback(new Error('Not allowed by CORS'));
                }
            },
        credentials: true,
        methods: ['GET', 'POST', 'PUT', 'DELETE', 'PATCH', 'OPTIONS'],
        allowedHeaders: [
            'Content-Type',
            'Authorization',
            'X-Requested-With',
            'X-Session-ID',
            'X-Admin-Password',
        ],
        exposedHeaders: ['X-Total-Count', 'X-Page-Count'],
        maxAge: 86400, // 24 小时
    };
}

module.exports = {
    configureHelmet,
    apiSecurityHeaders,
    corsConfig,
};
