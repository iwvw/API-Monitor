/**
 * 会话管理服务（使用 SQLite 数据库）
 */

const crypto = require('crypto');
const { parseCookies } = require('../utils/cookie');
const { Session } = require('../db/models');
const dbService = require('../db/database');
const { createLogger } = require('../utils/logger');

const logger = createLogger('Session');

// 初始化数据库
dbService.initialize();

/**
 * 从数据库加载 session（兼容旧接口）
 */
function loadSessions() {
  try {
    // 启动时先清理一次过期会话
    const cleaned = Session.cleanExpiredSessions();
    if (cleaned > 0) {
      logger.info(`清理了 ${cleaned} 个过期或无效会_话`);
    }

    const sessions = Session.getActiveSessions();
    logger.info(`已从数据库加载 ${sessions.length} 个活跃会话`);
  } catch (err) {
    logger.error('加载 session 失败:', err);
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
      logger.info(`自动清理了 ${cleaned} 个过期会话`);
    }
  } catch (err) {
    logger.error('清理过期会话失败:', err);
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
    expires_at: expiresAt.toISOString(),
  });

  logger.info(`创建新会话: ${sid.substring(0, 8)}...`);
  return sid;
}

/**
 * 获取 session
 */
function getSession(req) {
  const cookies = parseCookies(req);
  const sid = cookies.sid;

  if (!sid) {
    return null;
  }

  const validation = Session.validateSession(sid);

  if (!validation.valid) {
    logger.debug(`Session 无效 sid=${sid.substring(0, 8)}... 原因: ${validation.reason}`);
    return null;
  }

  logger.debug(`Session 有效 sid=${sid.substring(0, 8)}...`);

  const session = validation.session;
  return {
    sid: session.session_id,
    password: session.password,
    createdAt: session.created_at,
    lastAccessedAt: session.last_accessed_at,
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
    lastAccessedAt: session.last_accessed_at,
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
      logger.info(`销毁会话: ${sid.substring(0, 8)}...`);
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
  destroySession,
};
