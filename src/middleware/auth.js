/**
 * 认证中间件
 */

const { getSession, getSessionById } = require('../services/session');
const { loadAdminPassword } = require('../services/config');
const { createLogger } = require('../utils/logger');

const logger = createLogger('Auth');

/**
 * 密码/会话验证中间件
 */
function requireAuth(req, res, next) {
  // 1. 尝试从 Cookie 中获取 session
  const session = getSession(req);
  if (session) {
    // 静默通过，不输出日志
    return next();
  }

  // 2. 尝试从 Authorization header 中获取 sessionId
  const authHeader = req.headers['authorization'];
  if (authHeader && authHeader.startsWith('Bearer ')) {
    const sessionId = authHeader.substring(7);
    const headerSession = getSessionById(sessionId);
    if (headerSession) {
      // 静默通过，不输出日志
      return next();
    }
  }

  // 3. 回退到旧的 header 验证（保持兼容）
  const password = req.headers['x-admin-password'];
  const savedPassword = loadAdminPassword();

  if (!savedPassword) {
    // 如果没有设置密码，允许访问（首次设置）
    logger.debug('无密码设置，允许访问');
    return next();
  }

  if (password === savedPassword) {
    // 静默通过，不输出日志
    return next();
  }

  logger.warn(`认证失败: ${req.method} ${req.path}`);
  return res.status(401).json({ success: false, error: '未认证，请重新登录' });
}

module.exports = {
  requireAuth
};
