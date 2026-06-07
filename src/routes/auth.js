/**
 * 认证相关路由
 */

const express = require('express');
const router = express.Router();
const { createSession, destroySession, getSession } = require('../services/session');
const { createLogger } = require('../utils/logger');
const { verifyPasswordSync } = require('../utils/password');
const {
  loadAdminPassword,
  isPasswordSavedToFile,
  saveAdminPassword,
  isDemoMode,
} = require('../services/config');

// 导入安全中间件
const { authLimiter } = require('../middleware/rateLimit');
const { validate, loginSchema, changePasswordSchema } = require('../middleware/validation');
const { LoginAttempt, OperationLog } = require('../db/models');
const {
  get2FAStatus,
  generate2FASecret,
  enable2FA,
  disable2FA,
  is2FAEnabled,
} = require('../services/system-2fa');

const logger = createLogger('Auth');

/**
 * 检查是否已设置密码
 */
router.get('/check-password', (req, res) => {
  const savedPassword = loadAdminPassword();
  res.json({
    hasPassword: !!savedPassword,
    isDemoMode: isDemoMode(),
  });
});

/**
 * 登录：创建 session
 * 安全增强：
 * - 5次失败后锁定15分钟
 * - 使用 bcrypt 验证密码
 * - 支持 2FA 双因素认证
 * - 记录审计日志
 */
router.post('/login', authLimiter, validate({ body: loginSchema }), (req, res) => {
  const { password, totpToken } = req.body;
  const savedPassword = loadAdminPassword();
  const clientIp = req.ip || req.connection.remoteAddress || 'unknown';

  // 检查是否被锁定
  const lockStatus = LoginAttempt.isLocked(clientIp);
  if (lockStatus.locked) {
    const remainingMinutes = Math.ceil(lockStatus.remainingSeconds / 60);
    OperationLog.logOperation({
      operation_type: 'LOGIN_BLOCKED',
      table_name: 'auth',
      details: { ip: clientIp, reason: 'ip_locked' },
      ip_address: clientIp,
      user_agent: req.headers['user-agent'],
    });
    return res.status(429).json({
      success: false,
      error: `登录尝试过多，请 ${remainingMinutes} 分钟后再试`,
      lockUntil: lockStatus.lockUntil,
    });
  }

  if (isDemoMode()) {
    logger.info('演示模式：免密登录');
  } else {
    if (!savedPassword) {
      return res.status(400).json({ success: false, error: '请先设置管理员密码' });
    }

    if (!verifyPasswordSync(password, savedPassword)) {
      // 记录失败尝试
      const attemptResult = LoginAttempt.recordFailedAttempt(clientIp);
      OperationLog.logOperation({
        operation_type: 'LOGIN_FAILED',
        table_name: 'auth',
        details: { ip: clientIp, remainingAttempts: attemptResult.remainingAttempts },
        ip_address: clientIp,
        user_agent: req.headers['user-agent'],
      });

      if (attemptResult.locked) {
        return res.status(429).json({
          success: false,
          error: '登录尝试过多，账户已锁定 15 分钟',
          lockUntil: attemptResult.lockUntil,
        });
      }

      return res.status(401).json({
        success: false,
        error: `密码错误，还剩 ${attemptResult.remainingAttempts} 次尝试机会`,
      });
    }
  }

  // 密码验证通过，检查是否需要 2FA
  const { verifyLogin2FA } = require('../services/system-2fa');
  if (is2FAEnabled()) {
    if (!totpToken) {
      // 需要 2FA 但未提供验证码，返回提示
      return res.json({
        success: false,
        require2FA: true,
        error: '请输入双因素验证码',
      });
    }

    // 验证 2FA
    if (!verifyLogin2FA(totpToken)) {
      OperationLog.logOperation({
        operation_type: 'LOGIN_2FA_FAILED',
        table_name: 'auth',
        details: { ip: clientIp },
        ip_address: clientIp,
        user_agent: req.headers['user-agent'],
      });
      return res.status(401).json({
        success: false,
        error: '双因素验证码错误',
        require2FA: true,
      });
    }
  }

  // 登录成功，重置失败计数
  LoginAttempt.resetAttempts(clientIp);

  const sid = createSession(isDemoMode() ? 'demo' : 'authenticated');
  const cookieOptions = {
    httpOnly: true,
    sameSite: 'lax',
    path: '/',
    maxAge: 24 * 60 * 60 * 1000, // 24小时（毫秒）
  };

  // 记录登录成功
  OperationLog.logOperation({
    operation_type: 'LOGIN_SUCCESS',
    table_name: 'auth',
    details: { ip: clientIp, with2FA: is2FAEnabled() },
    ip_address: clientIp,
    user_agent: req.headers['user-agent'],
  });

  logger.info(`用户登录成功 sid=${sid.substring(0, 8)}... IP=${clientIp} 2FA=${is2FAEnabled()}`);
  res.cookie('sid', sid, cookieOptions);
  res.json({ success: true, sessionId: sid });
});

/**
 * 登出：销毁 session
 */
router.post('/logout', (req, res) => {
  const clientIp = req.ip || req.connection.remoteAddress || 'unknown';

  destroySession(req);

  // 记录登出
  OperationLog.logOperation({
    operation_type: 'LOGOUT',
    table_name: 'auth',
    details: { ip: clientIp },
    ip_address: clientIp,
    user_agent: req.headers['user-agent'],
  });

  res.clearCookie('sid', { httpOnly: true, sameSite: 'lax', path: '/' });
  res.json({ success: true });
});

/**
 * 会话检查
 */
router.get('/session', (req, res) => {
  const session = getSession(req);
  logger.debug(`Session 状态检查: ${session ? '已认证' : '未认证'}`);
  res.json({ authenticated: !!session });
});

/**
 * 设置管理员密码（首次）
 */
router.post('/set-password', (req, res) => {
  const { password } = req.body;

  if (isDemoMode()) {
    return res.status(403).json({ error: '演示模式禁止设置密码' });
  }

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
    logger.info('管理员密码已成功初始化');
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

  if (verifyPasswordSync(password, savedPassword)) {
    res.json({ success: true });
  } else {
    res.status(401).json({ success: false, error: '密码错误' });
  }
});

// 修改密码
router.post('/change-password', validate({ body: changePasswordSchema }), (req, res) => {
  const { oldPassword, newPassword } = req.body;
  const clientIp = req.ip || req.connection.remoteAddress || 'unknown';

  if (isDemoMode()) {
    return res.status(403).json({ success: false, error: '演示模式禁止修改密码' });
  }

  const savedPassword = loadAdminPassword();

  if (!savedPassword) {
    return res.status(400).json({ success: false, error: '请先设置密码' });
  }

  // 验证旧密码
  if (!verifyPasswordSync(oldPassword, savedPassword)) {
    OperationLog.logOperation({
      operation_type: 'PASSWORD_CHANGE_FAILED',
      table_name: 'auth',
      details: { ip: clientIp, reason: 'wrong_old_password' },
      ip_address: clientIp,
      user_agent: req.headers['user-agent'],
    });
    return res.status(401).json({ success: false, error: '原密码错误' });
  }

  // 验证新密码
  if (!newPassword || newPassword.length < 6) {
    return res.status(400).json({ success: false, error: '新密码长度至少6位' });
  }

  // 保存新密码
  if (saveAdminPassword(newPassword)) {
    OperationLog.logOperation({
      operation_type: 'PASSWORD_CHANGED',
      table_name: 'auth',
      details: { ip: clientIp },
      ip_address: clientIp,
      user_agent: req.headers['user-agent'],
    });
    logger.info('管理员密码已通过控制面板修改');
    res.json({ success: true });
  } else {
    res.status(500).json({ success: false, error: '保存密码失败' });
  }
});
// ==================== 2FA 相关路由 ====================

/**
 * 获取 2FA 状态
 */
router.get('/2fa/status', (req, res) => {
  const status = get2FAStatus();
  res.json({ success: true, ...status });
});

/**
 * 生成 2FA 密钥和二维码（设置流程第一步）
 */
router.post('/2fa/setup', (req, res) => {
  if (isDemoMode()) {
    return res.status(403).json({ success: false, error: '演示模式禁止设置 2FA' });
  }

  // 检查 session
  const session = getSession(req);
  if (!session) {
    return res.status(401).json({ success: false, error: '请先登录' });
  }

  generate2FASecret('admin')
    .then((result) => {
      res.json({
        success: true,
        secret: result.secret,
        qrCode: result.qrCodeDataUrl,
      });
    })
    .catch((error) => {
      res.status(500).json({ success: false, error: error.message });
    });
});

/**
 * 启用 2FA（验证并保存）
 */
router.post('/2fa/enable', (req, res) => {
  const { secret, token } = req.body;
  const clientIp = req.ip || req.connection.remoteAddress || 'unknown';

  if (isDemoMode()) {
    return res.status(403).json({ success: false, error: '演示模式禁止设置 2FA' });
  }

  if (!secret || !token) {
    return res.status(400).json({ success: false, error: '缺少必要参数' });
  }

  const session = getSession(req);
  if (!session) {
    return res.status(401).json({ success: false, error: '请先登录' });
  }

  const success = enable2FA(secret, token);
  if (success) {
    OperationLog.logOperation({
      operation_type: '2FA_ENABLED',
      table_name: 'auth',
      details: { ip: clientIp },
      ip_address: clientIp,
      user_agent: req.headers['user-agent'],
    });
    res.json({ success: true, message: '2FA 已启用' });
  } else {
    res.status(400).json({ success: false, error: '验证码错误，请重试' });
  }
});

/**
 * 禁用 2FA
 */
router.post('/2fa/disable', (req, res) => {
  const { password } = req.body;
  const clientIp = req.ip || req.connection.remoteAddress || 'unknown';

  if (isDemoMode()) {
    return res.status(403).json({ success: false, error: '演示模式禁止禁用 2FA' });
  }

  const session = getSession(req);
  if (!session) {
    return res.status(401).json({ success: false, error: '请先登录' });
  }

  // 验证密码
  const savedPassword = loadAdminPassword();
  if (!verifyPasswordSync(password, savedPassword)) {
    return res.status(401).json({ success: false, error: '密码错误' });
  }

  const success = disable2FA();
  if (success) {
    OperationLog.logOperation({
      operation_type: '2FA_DISABLED',
      table_name: 'auth',
      details: { ip: clientIp },
      ip_address: clientIp,
      user_agent: req.headers['user-agent'],
    });
    res.json({ success: true, message: '2FA 已禁用' });
  } else {
    res.status(500).json({ success: false, error: '禁用 2FA 失败' });
  }
});

module.exports = router;
