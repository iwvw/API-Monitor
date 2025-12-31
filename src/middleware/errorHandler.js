/**
 * 统一错误处理中间件
 * 提供标准化的错误响应格式
 */

const { createLogger } = require('../utils/logger');

const logger = createLogger('ErrorHandler');

/**
 * 自定义应用错误类
 */
class AppError extends Error {
    constructor(message, statusCode = 500, code = 'INTERNAL_ERROR') {
        super(message);
        this.statusCode = statusCode;
        this.code = code;
        this.isOperational = true;

        Error.captureStackTrace(this, this.constructor);
    }
}

/**
 * 常用错误类型
 */
class NotFoundError extends AppError {
    constructor(message = '资源未找到') {
        super(message, 404, 'NOT_FOUND');
    }
}

class BadRequestError extends AppError {
    constructor(message = '请求参数错误') {
        super(message, 400, 'BAD_REQUEST');
    }
}

class UnauthorizedError extends AppError {
    constructor(message = '未授权访问') {
        super(message, 401, 'UNAUTHORIZED');
    }
}

class ForbiddenError extends AppError {
    constructor(message = '禁止访问') {
        super(message, 403, 'FORBIDDEN');
    }
}

class ConflictError extends AppError {
    constructor(message = '资源冲突') {
        super(message, 409, 'CONFLICT');
    }
}

class ValidationError extends AppError {
    constructor(message = '数据验证失败', errors = []) {
        super(message, 422, 'VALIDATION_ERROR');
        this.errors = errors;
    }
}

class RateLimitError extends AppError {
    constructor(message = '请求过于频繁，请稍后再试') {
        super(message, 429, 'RATE_LIMIT_EXCEEDED');
    }
}

/**
 * 错误处理中间件
 */
function errorHandler(err, req, res, next) {
    // 如果响应已经发送，交给默认处理
    if (res.headersSent) {
        return next(err);
    }

    // 确定错误状态码
    let statusCode = err.statusCode || 500;
    let code = err.code || 'INTERNAL_ERROR';
    let message = err.message || '服务器内部错误';

    // 处理特定类型的错误
    if (err.name === 'SyntaxError' && err.status === 400) {
        // JSON 解析错误
        statusCode = 400;
        code = 'INVALID_JSON';
        message = 'JSON 格式无效';
    } else if (err.name === 'ValidationError' && err.errors) {
        // Mongoose/其他验证错误
        statusCode = 422;
        code = 'VALIDATION_ERROR';
    } else if (err.code === 'SQLITE_CONSTRAINT') {
        // SQLite 约束错误
        statusCode = 409;
        code = 'DATABASE_CONSTRAINT';
        message = '数据约束冲突';
    }

    // 生产环境隐藏内部错误详情
    const isDev = process.env.NODE_ENV !== 'production';

    // 记录错误日志
    if (statusCode >= 500) {
        logger.error(`${req.method} ${req.path} - ${statusCode} ${code}: ${message}`, {
            stack: isDev ? err.stack : undefined,
            body: isDev ? req.body : undefined,
        });
    } else {
        logger.warn(`${req.method} ${req.path} - ${statusCode} ${code}: ${message}`);
    }

    // 构建响应
    const response = {
        success: false,
        error: {
            code,
            message: statusCode >= 500 && !isDev ? '服务器内部错误' : message,
        },
    };

    // 添加验证错误详情
    if (err.errors && Array.isArray(err.errors)) {
        response.error.details = err.errors;
    }

    // 开发环境添加堆栈信息
    if (isDev && err.stack) {
        response.error.stack = err.stack.split('\n').slice(0, 5);
    }

    res.status(statusCode).json(response);
}

/**
 * 404 处理中间件
 */
function notFoundHandler(req, res, next) {
    // 跳过静态文件和 API 之外的请求
    if (req.path.startsWith('/api/')) {
        const error = new NotFoundError(`接口 ${req.method} ${req.path} 不存在`);
        next(error);
    } else {
        // 非 API 请求交给下一个处理器（可能是 SPA 回退）
        next();
    }
}

/**
 * 异步路由包装器
 * 自动捕获异步路由中的错误
 */
function asyncHandler(fn) {
    return (req, res, next) => {
        Promise.resolve(fn(req, res, next)).catch(next);
    };
}

/**
 * 创建标准成功响应
 */
function successResponse(res, data = null, message = '操作成功', statusCode = 200) {
    const response = {
        success: true,
        message,
    };

    if (data !== null) {
        response.data = data;
    }

    return res.status(statusCode).json(response);
}

/**
 * 创建分页响应
 */
function paginatedResponse(res, data, pagination, message = '获取成功') {
    return res.status(200).json({
        success: true,
        message,
        data,
        pagination: {
            page: pagination.page,
            pageSize: pagination.pageSize,
            total: pagination.total,
            totalPages: Math.ceil(pagination.total / pagination.pageSize),
        },
    });
}

module.exports = {
    // 错误类
    AppError,
    NotFoundError,
    BadRequestError,
    UnauthorizedError,
    ForbiddenError,
    ConflictError,
    ValidationError,
    RateLimitError,

    // 中间件
    errorHandler,
    notFoundHandler,
    asyncHandler,

    // 响应辅助函数
    successResponse,
    paginatedResponse,
};
