/**
 * 认证相关路由
 */

const express = require('express');
const router = express.Router();
const { createSession, destroySession, getSession } = require('../services/session');
const {
  loadAdminPassword,
  isPasswordSavedToFile,
  saveAdminPassword
} = require('../services/config');

/**
 * 检查是否已设置密码
 */
router.get('/check-password', (req, res) => {
  const savedPassword = loadAdminPassword();
  res.json({ hasPassword: !!savedPassword });
});

/**
 * 登录：创建 session
 */
router.post('/login', (req, res) => {
  const { password } = req.body;
  const savedPassword = loadAdminPassword();

  if (!savedPassword) {
    return res.status(400).json({ success: false, error: '请先设置管理员密码' });
  }

  if (password !== savedPassword) {
    return res.status(401).json({ success: false, error: '密码错误' });
  }

  const sid = createSession(password);
  const cookieOptions = {
    httpOnly: true,
    sameSite: 'lax',
    path: '/'
  };

  console.log(`✅ 创建会话 sid=${sid.substring(0, 8)}... (永久保存)`);
  res.cookie('sid', sid, cookieOptions);
  res.json({ success: true, sessionId: sid });
});

/**
 * 登出：销毁 session
 */
router.post('/logout', (req, res) => {
  destroySession(req);
  res.cookie('sid', '', { httpOnly: true, maxAge: 0, path: '/' });
  res.json({ success: true });
});

/**
 * 会话检查
 */
router.get('/session', (req, res) => {
  const session = getSession(req);
  console.log(`🔍 /api/session 检查 - 认证状态:`, !!session);
  res.json({ authenticated: !!session });
});

/**
 * 设置管理员密码（首次）
 */
router.post('/set-password', (req, res) => {
  const { password } = req.body;

  if (process.env.ADMIN_PASSWORD) {
    return res.status(400).json({ error: '密码已通过环境变量设置，无法修改' });
  }

  if (isPasswordSavedToFile()) {
    return res.status(400).json({ error: '密码已设置，无法重复设置' });
  }

  if (!password || password.length < 6) {
    return res.status(400).json({ error: '密码长度至少6位' });
  }

  if (saveAdminPassword(password)) {
    console.log('✅ 管理员密码已设置');
    res.json({ success: true });
  } else {
    res.status(500).json({ error: '保存密码失败' });
  }
});

/**
 * 验证密码
 */
router.post('/verify-password', (req, res) => {
  const { password } = req.body;
  const savedPassword = loadAdminPassword();

  if (!savedPassword) {
    return res.status(400).json({ success: false, error: '请先设置密码' });
  }

  if (password === savedPassword) {
    res.json({ success: true });
  } else {
    res.status(401).json({ success: false, error: '密码错误' });
  }
});

/**
 * 修改密码
 */
router.post('/change-password', (req, res) => {
  const { oldPassword, newPassword } = req.body;
  const savedPassword = loadAdminPassword();

  if (!savedPassword) {
    return res.status(400).json({ success: false, error: '请先设置密码' });
  }

  // 验证旧密码
  if (oldPassword !== savedPassword) {
    return res.status(401).json({ success: false, error: '原密码错误' });
  }

  // 验证新密码
  if (!newPassword || newPassword.length < 6) {
    return res.status(400).json({ success: false, error: '新密码长度至少6位' });
  }

  // 保存新密码
  if (saveAdminPassword(newPassword)) {
    console.log('✅ 管理员密码已修改');
    res.json({ success: true });
  } else {
    res.status(500).json({ success: false, error: '保存密码失败' });
  }
});

module.exports = router;
