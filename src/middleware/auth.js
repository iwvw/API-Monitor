/**
 * 认证中间件
 */

const { getSession, getSessionById } = require('../services/session');
const { loadAdminPassword } = require('../services/config');

/**
 * 密码/会话验证中间件
 */
function requireAuth(req, res, next) {
  // 1. 尝试从 Cookie 中获取 session
  const session = getSession(req);
  if (session) {
    console.log(`✅ session 认证通过 (cookie)`);
    return next();
  }

  // 2. 尝试从 Authorization header 中获取 sessionId
  const authHeader = req.headers['authorization'];
  if (authHeader && authHeader.startsWith('Bearer ')) {
    const sessionId = authHeader.substring(7);
    const headerSession = getSessionById(sessionId);
    if (headerSession) {
      console.log(`✅ session 认证通过 (header) sid=${sessionId.substring(0, 8)}...`);
      return next();
    }
  }

  // 3. 回退到旧的 header 验证（保持兼容）
  const password = req.headers['x-admin-password'];
  const savedPassword = loadAdminPassword();

  if (!savedPassword) {
    // 如果没有设置密码，允许访问（首次设置）
    console.log(`ℹ️ 无密码设置，允许访问`);
    return next();
  }

  if (password === savedPassword) {
    console.log(`✅ header 密码认证通过`);
    return next();
  }

  console.log(`❌ 认证失败：无有效 session 或密码`);
  return res.status(401).json({ success: false, error: '未认证，请重新登录' });
}

module.exports = {
  requireAuth
};
