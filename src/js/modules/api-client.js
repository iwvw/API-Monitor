/**
 * API Client - 统一的 API 请求封装
 * 提供标准化的请求方法和错误处理
 */

import { store } from '../store.js';
import toastManager from './toast.js';

/**
 * 标准 API 响应格式
 * @typedef {Object} ApiResponse
 * @property {boolean} success - 请求是否成功
 * @property {*} [data] - 响应数据
 * @property {string} [error] - 错误信息
 * @property {string} [code] - 错误码
 */

/**
 * API 错误类
 */
export class ApiError extends Error {
  constructor(message, code, status, response) {
    super(message);
    this.name = 'ApiError';
    this.code = code;
    this.status = status;
    this.response = response;
  }
}

/**
 * 获取认证头
 */
function getAuthHeaders() {
  const headers = {
    'Content-Type': 'application/json',
  };

  const password = localStorage.getItem('admin_password') || store.getState?.().loginPassword;
  if (password) {
    headers['x-admin-password'] = password;
  }

  return headers;
}

/**
 * 处理响应
 */
async function handleResponse(response) {
  const contentType = response.headers.get('content-type');

  // 处理 JSON 响应
  if (contentType && contentType.includes('application/json')) {
    const data = await response.json();

    if (!response.ok) {
      throw new ApiError(
        data.error || `请求失败 (${response.status})`,
        data.code,
        response.status,
        data
      );
    }

    // 标准化响应格式
    if (data.success !== undefined) {
      if (!data.success) {
        throw new ApiError(
          data.error || '操作失败',
          data.code,
          response.status,
          data
        );
      }
      return data.data !== undefined ? data.data : data;
    }

    return data;
  }

  // 处理非 JSON 响应
  if (!response.ok) {
    const text = await response.text();
    throw new ApiError(
      text || `请求失败 (${response.status})`,
      null,
      response.status,
      null
    );
  }

  return response;
}

/**
 * 处理错误
 */
function handleError(error, options = {}) {
  const { silent = false, showToast = true } = options;

  // 网络错误
  if (error instanceof TypeError && error.message.includes('fetch')) {
    const message = '网络连接失败，请检查网络';
    if (showToast) toastManager.error(message);
    throw new ApiError(message, 'NETWORK_ERROR', 0, null);
  }

  // API 错误
  if (error instanceof ApiError) {
    if (!silent && showToast) {
      // 特殊错误码处理
      if (error.status === 401) {
        toastManager.warning('登录已过期，请重新登录');
      } else if (error.status === 403) {
        toastManager.error('没有权限访问');
      } else if (error.status === 404) {
        toastManager.error('请求的资源不存在');
      } else if (error.status === 429) {
        toastManager.warning('请求过于频繁，请稍后再试');
      } else if (error.status >= 500) {
        toastManager.error('服务器错误，请稍后重试');
      } else {
        toastManager.error(error.message);
      }
    }
    throw error;
  }

  // 其他错误
  const message = error.message || '未知错误';
  if (showToast) toastManager.error(message);
  throw new ApiError(message, 'UNKNOWN_ERROR', 0, null);
}

/**
 * GET 请求
 */
export async function get(url, options = {}) {
  try {
    const response = await fetch(url, {
      method: 'GET',
      headers: getAuthHeaders(),
      ...options,
    });
    return await handleResponse(response);
  } catch (error) {
    return handleError(error, options);
  }
}

/**
 * POST 请求
 */
export async function post(url, data, options = {}) {
  try {
    const response = await fetch(url, {
      method: 'POST',
      headers: getAuthHeaders(),
      body: JSON.stringify(data),
      ...options,
    });
    return await handleResponse(response);
  } catch (error) {
    return handleError(error, options);
  }
}

/**
 * PUT 请求
 */
export async function put(url, data, options = {}) {
  try {
    const response = await fetch(url, {
      method: 'PUT',
      headers: getAuthHeaders(),
      body: JSON.stringify(data),
      ...options,
    });
    return await handleResponse(response);
  } catch (error) {
    return handleError(error, options);
  }
}

/**
 * PATCH 请求
 */
export async function patch(url, data, options = {}) {
  try {
    const response = await fetch(url, {
      method: 'PATCH',
      headers: getAuthHeaders(),
      body: JSON.stringify(data),
      ...options,
    });
    return await handleResponse(response);
  } catch (error) {
    return handleError(error, options);
  }
}

/**
 * DELETE 请求
 */
export async function del(url, options = {}) {
  try {
    const response = await fetch(url, {
      method: 'DELETE',
      headers: getAuthHeaders(),
      ...options,
    });
    return await handleResponse(response);
  } catch (error) {
    return handleError(error, options);
  }
}

/**
 * 文件上传
 */
export async function upload(url, formData, options = {}) {
  try {
    const headers = {};
    const password = localStorage.getItem('admin_password') || store.getState?.().loginPassword;
    if (password) {
      headers['x-admin-password'] = password;
    }

    const response = await fetch(url, {
      method: 'POST',
      headers,
      body: formData,
      ...options,
    });
    return await handleResponse(response);
  } catch (error) {
    return handleError(error, options);
  }
}

/**
 * 默认导出
 */
export default {
  get,
  post,
  put,
  patch,
  delete: del,
  upload,
  ApiError,
};
