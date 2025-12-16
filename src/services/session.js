/**
 * 会话管理服务（使用 SQLite 数据库）
 */

const crypto = require('crypto');
const { parseCookies } = require('../utils/cookie');
const { Session } = require('../db/models');
const dbService = require('../db/database');

// 初始化数据库
dbService.initialize();

/**
 * 从数据库加载 session（兼容旧接口）
 */
function loadSessions() {
  try {
    const sessions = Session.getActiveSessions();
    console.log('✅ 已从数据库加载 session，数量:', sessions.length);
  } catch (err) {
    console.error('❌ 加载 session 失败:', err.message);
  }
}

/**
 * 保存 session 到数据库（兼容旧接口，实际上数据库自动保存）
 */
function saveSessions() {
  // 数据库自动保存，此函数保留用于兼容性
  // 可以在这里执行清理过期 session 的操作
  try {
    const cleaned = Session.cleanExpiredSessions();
    if (cleaned > 0) {
      console.log(`🧹 清理了 ${cleaned} 个过期 session`);
    }
  } catch (err) {
    console.error('❌ 清理过期 session 失败:', err.message);
  }
}

/**
 * 创建新 session
 */
function createSession(password) {
  const sid = crypto.randomBytes(24).toString('hex');

  const expiresAt = new Date();
  expiresAt.setHours(expiresAt.getHours() + 24); // 24小时后过期

  Session.createSession({
    session_id: sid,
    password: password,
    expires_at: expiresAt.toISOString()
  });

  console.log('✨ 创建新 session:', sid.substring(0, 8) + '...');
  return sid;
}

/**
 * 获取 session
 */
function getSession(req) {
  const cookies = parseCookies(req);
  const sid = cookies.sid;

  if (!sid) {
    console.log('⚠️ 无 session cookie');
    return null;
  }

  const validation = Session.validateSession(sid);

  if (!validation.valid) {
    console.log(`⚠️ session 无效 sid=${sid.substring(0, 8)}... 原因: ${validation.reason}`);
    return null;
  }

  console.log(`✓ session 有效 sid=${sid.substring(0, 8)}... (数据库存储)`);

  const session = validation.session;
  return {
    sid: session.session_id,
    password: session.password,
    createdAt: session.created_at,
    lastAccessedAt: session.last_accessed_at
  };
}

/**
 * 通过 sessionId 获取 session
 */
function getSessionById(sessionId) {
  if (!sessionId) {
    return null;
  }

  const validation = Session.validateSession(sessionId);

  if (!validation.valid) {
    return null;
  }

  const session = validation.session;
  return {
    sid: session.session_id,
    password: session.password,
    createdAt: session.created_at,
    lastAccessedAt: session.last_accessed_at
  };
}

/**
 * 销毁 session
 */
function destroySession(req) {
  const cookies = parseCookies(req);
  const sid = cookies.sid;

  if (sid) {
    const session = Session.getSession(sid);
    if (session) {
      Session.invalidateSession(sid);
      console.log('🔒 销毁 session:', sid.substring(0, 8) + '...');
      return true;
    }
  }

  return false;
}

module.exports = {
  loadSessions,
  saveSessions,
  createSession,
  getSession,
  getSessionById,
  destroySession
};
