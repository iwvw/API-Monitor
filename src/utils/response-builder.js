/**
 * API 响应构建工具
 * 提供统一的响应格式
 */

/**
 * 构建成功响应
 * @param {*} data - 响应数据
 * @param {string} [message] - 可选消息
 * @returns {Object} 标准成功响应对象
 *
 * @example
 * res.json(success({ id: 1, name: 'test' }))
 * // => { success: true, data: { id: 1, name: 'test' } }
 */
function success(data, message, meta = undefined) {
  const response = {
    success: true,
    data,
  };
  if (message) {
    response.message = message;
  }
  if (meta !== undefined) {
    response.meta = meta;
  }
  return response;
}

/**
 * 构建错误响应
 * @param {string} code - 错误代码
 * @param {string} message - 错误消息
 * @param {*} [details] - 可选详细信息
 * @returns {Object} 标准错误响应对象
 *
 * @example
 * res.status(400).json(error('VALIDATION_ERROR', 'Invalid input'))
 * // => { success: false, error: { code: 'VALIDATION_ERROR', message: 'Invalid input' } }
 */
function error(code, message, details, requestId = undefined) {
  const response = {
    success: false,
    error: {
      code,
      message,
    },
  };
  if (details !== undefined) {
    response.error.details = details;
  }
  if (requestId) {
    response.requestId = requestId;
  }
  return response;
}

/**
 * 构建分页响应
 * @param {Array} items - 数据项数组
 * @param {Object} pagination - 分页信息
 * @param {number} pagination.page - 当前页码
 * @param {number} pagination.pageSize - 每页数量
 * @param {number} pagination.total - 总数量
 * @returns {Object} 标准分页响应对象
 *
 * @example
 * res.json(paginated(users, { page: 1, pageSize: 10, total: 100 }))
 */
function paginated(items, { page, pageSize, total }) {
  return {
    success: true,
    data: {
      items,
      pagination: {
        page,
        pageSize,
        total,
        totalPages: Math.ceil(total / pageSize),
        hasMore: page * pageSize < total,
      },
    },
  };
}

/**
 * 发送成功响应
 * @param {Object} res - Express response 对象
 * @param {*} data - 响应数据
 * @param {number} [status=200] - HTTP 状态码
 */
function sendSuccess(res, data, status = 200) {
  res.status(status).json(success(data));
}

/**
 * 发送错误响应
 * @param {Object} res - Express response 对象
 * @param {number} status - HTTP 状态码
 * @param {string} code - 错误代码
 * @param {string} message - 错误消息
 * @param {*} [details] - 可选详细信息
 */
function sendError(res, status, code, message, details) {
  res.status(status).json(error(code, message, details));
}

function fromException(err, fallbackCode = 'INTERNAL_ERROR') {
  if (!err) {
    return error(fallbackCode, 'Unknown error');
  }

  const code = err.code || err.name || fallbackCode;
  const message = err.message || 'Internal Server Error';
  return error(code, message, err.details);
}

/**
 * 常用错误响应快捷方法
 */
const errors = {
  badRequest: (res, message = 'Bad Request', details) =>
    sendError(res, 400, 'BAD_REQUEST', message, details),

  unauthorized: (res, message = 'Unauthorized') => sendError(res, 401, 'UNAUTHORIZED', message),

  forbidden: (res, message = 'Forbidden') => sendError(res, 403, 'FORBIDDEN', message),

  notFound: (res, message = 'Not Found') => sendError(res, 404, 'NOT_FOUND', message),

  validationError: (res, message = 'Validation Error', details) =>
    sendError(res, 422, 'VALIDATION_ERROR', message, details),

  tooManyRequests: (res, message = 'Too Many Requests') =>
    sendError(res, 429, 'TOO_MANY_REQUESTS', message),

  internal: (res, message = 'Internal Server Error') =>
    sendError(res, 500, 'INTERNAL_ERROR', message),
};

module.exports = {
  success,
  error,
  paginated,
  sendSuccess,
  sendError,
  fromException,
  errors,
};
