const { v4: uuidv4 } = require('uuid');
const { createLogger, asyncLocalStorage } = require('../utils/logger');

const logger = createLogger('HTTP');

/**
 * 结构化日志中间件
 * 负责生成 Trace ID、记录请求耗时和审计信息
 */
function loggerMiddleware(req, res, next) {
    // 获取或生成 Trace ID
    const traceId = req.headers['x-trace-id'] || uuidv4();
    
    // 设置响应头以便客户端追踪
    res.setHeader('X-Trace-Id', traceId);

    // 获取客户端 IP
    const ip = req.headers['x-forwarded-for'] || req.socket.remoteAddress;

    // 在异步上下文中运行后续逻辑
    asyncLocalStorage.run({ traceId, ip, userId: req.session?.userId }, () => {
        const start = Date.now();

        // 记录请求开始 (可选，如果是 DEBUG 级别)
        logger.debug(`${req.method} ${req.url} started`, {
            ip,
            userAgent: req.headers['user-agent']
        });

        // 监听响应结束
        res.on('finish', () => {
            const duration = Date.now() - start;
            const status = res.statusCode;
            
            // 忽略静态资源和成功的探测请求以减少噪音
            const isStatic = /\.(js|css|png|jpg|jpeg|gif|ico|svg|woff2?|ttf|otf)$/.test(req.path);
            const isProbe = req.path === '/health' || req.path === '/api/server/check-all';
            
            if ((isStatic || isProbe) && status < 400) {
                return; // 直接忽略这些高频无意义日志
            }

            // 决定日志级别
            let logLevel = 'info';
            if (status >= 500) logLevel = 'error';
            else if (status >= 400) logLevel = 'warn';

            // 某些高频 API 即使成功也只用 debug
            if (status < 400 && (req.path.includes('/api/settings') || req.path.includes('/api/server/accounts'))) {
                logLevel = 'debug';
            }

            // 结构化日志输出
            const message = `${req.method.padEnd(6)} ${req.url} ${status} - ${duration}ms`;
            
            logger[logLevel](message, {
                method: req.method,
                path: req.path,
                status,
                duration,
                ip,
                contentLength: res.get('Content-Length')
            });
        });

        next();
    });
}

module.exports = loggerMiddleware;
