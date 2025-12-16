/**
 * 会话管理服务
 */

const crypto = require('crypto');
const fs = require('fs');
const path = require('path');
const { parseCookies } = require('../utils/cookie');
const { CONFIG_DIR } = require('./config');

const SESSIONS_FILE = path.join(CONFIG_DIR, 'sessions.json');

// Session 持久化存储
const sessions = Object.create(null);

/**
 * 从文件加载 session
 */
function loadSessions() {
  try {
    if (fs.existsSync(SESSIONS_FILE)) {
      const data = fs.readFileSync(SESSIONS_FILE, 'utf-8');
      const loaded = JSON.parse(data);
      Object.assign(sessions, loaded);
      console.log('✅ 已加载持久化 session，数量:', Object.keys(sessions).length);
    }
  } catch (err) {
    console.error('❌ 加载 session 失败:', err.message);
  }
}

/**
 * 保存 session 到文件
 */
function saveSessions() {
  try {
    // 确保配置目录存在
    if (!fs.existsSync(CONFIG_DIR)) {
      fs.mkdirSync(CONFIG_DIR, { recursive: true });
    }
    fs.writeFileSync(SESSIONS_FILE, JSON.stringify(sessions, null, 2));
  } catch (err) {
    console.error('❌ 保存 session 失败:', err.message);
  }
}

/**
 * 创建新 session
 */
function createSession(password) {
  const sid = crypto.randomBytes(24).toString('hex');
  sessions[sid] = {
    password: password,
    createdAt: new Date().toISOString(),
    lastAccessedAt: new Date().toISOString()
  };
  saveSessions();
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
  const session = sessions[sid];
  if (!session) {
    console.log(`⚠️ session 不存在 sid=${sid.substring(0, 8)}...`);
    return null;
  }
  // 更新访问时间
  session.lastAccessedAt = new Date().toISOString();
  saveSessions();
  console.log(`✓ session 有效 sid=${sid.substring(0, 8)}... (永久保存)`);
  return { sid, ...session };
}

/**
 * 通过 sessionId 获取 session
 */
function getSessionById(sessionId) {
  if (!sessionId || !sessions[sessionId]) {
    return null;
  }
  const session = sessions[sessionId];
  session.lastAccessedAt = new Date().toISOString();
  saveSessions();
  return { sid: sessionId, ...session };
}

/**
 * 销毁 session
 */
function destroySession(req) {
  const cookies = parseCookies(req);
  const sid = cookies.sid;
  if (sid && sessions[sid]) {
    delete sessions[sid];
    saveSessions();
    console.log('🔒 销毁 session:', sid.substring(0, 8) + '...');
    return true;
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
