/**
 * TOTP/HOTP 模块 API 路由
 */

const express = require('express');
const router = express.Router();
const storage = require('./storage');
const totpService = require('./totp-service');
const { createLogger } = require('../../src/utils/logger');

const logger = createLogger('TOTP');

// ==================== 账号 API ====================

/**
 * GET /accounts
 * 获取所有账号（不含密钥）
 * 支持 ?withCodes=true 参数同时返回实时验证码（用于浏览器扩展）
 */
router.get('/accounts', async (req, res) => {
  try {
    const accounts = storage.loadAccounts();
    const withCodes = req.query.withCodes === 'true';

    // 如果需要验证码，先批量生成
    let codes = {};
    if (withCodes) {
      codes = totpService.generateAllCodes(accounts);
    }

    const safeAccounts = accounts.map(acc => ({
      ...acc,
      secret: undefined,
      hasSecret: !!acc.secret,
      // 附加验证码（如果请求了）
      currentCode: withCodes && codes[acc.id] ? codes[acc.id].code : undefined,
      remaining: withCodes && codes[acc.id] ? codes[acc.id].remaining : undefined,
    }));
    res.json({ success: true, data: safeAccounts });
  } catch (error) {
    logger.error('获取账号列表失败', error.message);
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * POST /accounts
 * 创建新账号
 */
router.post('/accounts', async (req, res) => {
  try {
    const {
      otp_type,
      issuer,
      account,
      secret,
      algorithm,
      digits,
      period,
      counter,
      group_id,
      icon,
      color,
    } = req.body;

    if (!secret) {
      return res.status(400).json({ success: false, error: '密钥不能为空' });
    }

    const base32Regex = /^[A-Z2-7]+=*$/i;
    const cleanSecret = secret.replace(/\s/g, '').toUpperCase();

    if (!base32Regex.test(cleanSecret)) {
      return res.status(400).json({ success: false, error: '无效的 Base32 密钥格式' });
    }

    // 验证密钥
    const testAccount = { otp_type, secret: cleanSecret, digits, period, counter };
    const testResult = totpService.generateCode(testAccount);
    if (!testResult.code) {
      return res.status(400).json({ success: false, error: '密钥无效，无法生成验证码' });
    }

    const newAccount = storage.createAccount({
      otp_type: otp_type || 'totp',
      issuer: issuer || '未知',
      account: account || '',
      secret: cleanSecret,
      algorithm: algorithm || 'SHA1',
      digits: digits || 6,
      period: period || 30,
      counter: counter || 0,
      group_id,
      icon,
      color,
    });

    logger.success(`创建账号: ${newAccount.issuer} (${otp_type || 'totp'})`);

    res.json({
      success: true,
      data: { ...newAccount, secret: undefined },
    });
  } catch (error) {
    logger.error('创建账号失败', error.message);
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * PUT /accounts/:id
 * 更新账号信息
 */
router.put('/accounts/:id', async (req, res) => {
  try {
    const { id } = req.params;
    const updates = req.body;

    const existing = storage.getAccount(id);
    if (!existing) {
      return res.status(404).json({ success: false, error: '账号不存在' });
    }

    if (updates.secret) {
      const cleanSecret = updates.secret.replace(/\s/g, '').toUpperCase();
      const testAccount = { ...existing, secret: cleanSecret };
      const testResult = totpService.generateCode(testAccount);
      if (!testResult.code) {
        return res.status(400).json({ success: false, error: '新密钥无效' });
      }
      updates.secret = cleanSecret;
    }

    storage.updateAccount(id, updates);
    logger.info(`更新账号: ${id}`);

    res.json({ success: true });
  } catch (error) {
    logger.error('更新账号失败', error.message);
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * DELETE /accounts/:id
 */
router.delete('/accounts/:id', async (req, res) => {
  try {
    const { id } = req.params;
    const existing = storage.getAccount(id);
    if (!existing) {
      return res.status(404).json({ success: false, error: '账号不存在' });
    }

    storage.deleteAccount(id);
    logger.info(`删除账号: ${id}`);
    res.json({ success: true });
  } catch (error) {
    logger.error('删除账号失败', error.message);
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * POST /accounts/:id/increment
 * HOTP 递增计数器并返回新验证码
 */
router.post('/accounts/:id/increment', async (req, res) => {
  try {
    const { id } = req.params;
    const account = storage.getAccount(id);

    if (!account) {
      return res.status(404).json({ success: false, error: '账号不存在' });
    }

    if (account.otp_type !== 'hotp') {
      return res.status(400).json({ success: false, error: '仅 HOTP 账号支持递增' });
    }

    const newCounter = totpService.incrementCounter(account.counter);
    storage.updateAccount(id, { counter: newCounter });

    const code = totpService.generateHotpCode(account.secret, newCounter, {
      digits: account.digits,
    });

    logger.info(`HOTP 递增: ${account.issuer} -> ${newCounter}`);
    res.json({ success: true, data: { code, counter: newCounter } });
  } catch (error) {
    logger.error('HOTP 递增失败', error.message);
    res.status(500).json({ success: false, error: error.message });
  }
});

// ==================== 验证码 API ====================

/**
 * GET /codes
 * 批量获取所有账号的当前验证码
 */
router.get('/codes', async (req, res) => {
  try {
    const accounts = storage.loadAccounts();
    const codes = totpService.generateAllCodes(accounts);
    res.json({ success: true, data: codes });
  } catch (error) {
    logger.error('获取验证码失败', error.message);
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * GET /accounts/:id/code
 */
router.get('/accounts/:id/code', async (req, res) => {
  try {
    const { id } = req.params;
    const account = storage.getAccount(id);

    if (!account) {
      return res.status(404).json({ success: false, error: '账号不存在' });
    }

    const result = totpService.generateCode(account);
    res.json({ success: true, data: result });
  } catch (error) {
    logger.error('获取验证码失败', error.message);
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * POST /verify
 */
router.post('/verify', async (req, res) => {
  try {
    const { id, token } = req.body;

    if (!id || !token) {
      return res.status(400).json({ success: false, error: '缺少参数' });
    }

    const account = storage.getAccount(id);
    if (!account) {
      return res.status(404).json({ success: false, error: '账号不存在' });
    }

    let isValid = false;
    if (account.otp_type === 'hotp') {
      const result = totpService.verifyHotpCode(account.secret, token, account.counter, {
        digits: account.digits,
      });
      isValid = result.valid;
      if (isValid) {
        storage.updateAccount(id, { counter: result.newCounter });
      }
    } else {
      isValid = totpService.verifyTotpCode(account.secret, token, {
        digits: account.digits,
        period: account.period,
      });
    }

    res.json({ success: true, valid: isValid });
  } catch (error) {
    logger.error('验证码验证失败', error.message);
    res.status(500).json({ success: false, error: error.message });
  }
});

// ==================== 分组 API ====================

/**
 * GET /groups
 */
router.get('/groups', async (req, res) => {
  try {
    const groups = storage.loadGroups();
    res.json({ success: true, data: groups });
  } catch (error) {
    logger.error('获取分组失败', error.message);
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * POST /groups
 */
router.post('/groups', async (req, res) => {
  try {
    const { name, icon, color } = req.body;
    if (!name) {
      return res.status(400).json({ success: false, error: '分组名称不能为空' });
    }

    const group = storage.createGroup({ name, icon, color });
    logger.success(`创建分组: ${name}`);
    res.json({ success: true, data: group });
  } catch (error) {
    logger.error('创建分组失败', error.message);
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * PUT /groups/:id
 */
router.put('/groups/:id', async (req, res) => {
  try {
    const { id } = req.params;
    storage.updateGroup(id, req.body);
    logger.info(`更新分组: ${id}`);
    res.json({ success: true });
  } catch (error) {
    logger.error('更新分组失败', error.message);
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * DELETE /groups/:id
 */
router.delete('/groups/:id', async (req, res) => {
  try {
    const { id } = req.params;
    storage.deleteGroup(id);
    logger.info(`删除分组: ${id}`);
    res.json({ success: true });
  } catch (error) {
    logger.error('删除分组失败', error.message);
    res.status(500).json({ success: false, error: error.message });
  }
});

// ==================== 导入/导出 ====================

/**
 * GET /export
 * 导出所有账号的 OTP URI
 */
router.get('/export', async (req, res) => {
  try {
    const accounts = storage.loadAccounts();
    const uris = accounts.map(acc => totpService.generateUri(acc));
    res.json({ success: true, data: uris });
  } catch (error) {
    logger.error('导出失败', error.message);
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * POST /import
 */
router.post('/import', async (req, res) => {
  try {
    const { uris, accounts: rawAccounts } = req.body;
    const toImport = [];

    if (Array.isArray(uris)) {
      for (const uri of uris) {
        const parsed = totpService.parseUri(uri);
        if (parsed) {
          toImport.push(parsed);
        }
      }
    }

    if (Array.isArray(rawAccounts)) {
      toImport.push(...rawAccounts);
    }

    if (toImport.length === 0) {
      return res.status(400).json({ success: false, error: '没有有效的导入数据' });
    }

    const results = storage.importAccounts(toImport);
    logger.info(`导入账号: 成功 ${results.success}, 失败 ${results.failed}`);
    res.json({ success: true, data: results });
  } catch (error) {
    logger.error('导入失败', error.message);
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * PUT /order
 */
router.put('/order', async (req, res) => {
  try {
    const { orderedIds } = req.body;
    if (!Array.isArray(orderedIds)) {
      return res.status(400).json({ success: false, error: '无效的排序数据' });
    }

    storage.updateOrder(orderedIds);
    logger.info('更新账号排序');
    res.json({ success: true });
  } catch (error) {
    logger.error('更新排序失败', error.message);
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * POST /generate-secret
 */
router.post('/generate-secret', async (req, res) => {
  try {
    const secret = totpService.generateSecret();
    res.json({ success: true, data: { secret } });
  } catch (error) {
    logger.error('生成密钥失败', error.message);
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * GET /extension/download
 * 下载浏览器扩展程序 ZIP
 */
router.get('/extension/download', async (req, res) => {
  try {
    const path = require('path');
    const fs = require('fs');
    const { exec } = require('child_process');

    const pluginDir = path.join(__dirname, '../../plugin');
    const tempDir = path.join(__dirname, '../../tmp');
    const zipFile = path.join(tempDir, 'api-monitor-2fa-extension.zip');

    if (!fs.existsSync(tempDir)) fs.mkdirSync(tempDir);

    // 使用 PowerShell 进行压缩
    const cmd = `powershell -Command "Compress-Archive -Path '${pluginDir}\\*' -DestinationPath '${zipFile}' -Force"`;

    exec(cmd, (error, stdout, stderr) => {
      if (error) {
        logger.error('压缩扩展失败', error.message);
        return res.status(500).json({ success: false, error: '压缩失败' });
      }

      res.download(zipFile, 'api-monitor-2fa-extension.zip', err => {
        if (err) logger.error('发送扩展失败', err.message);
        // 发送后删除临时文件
        try {
          fs.unlinkSync(zipFile);
        } catch (e) {}
      });
    });
  } catch (error) {
    logger.error('下载扩展异常', error.message);
    res.status(500).json({ success: false, error: error.message });
  }
});

module.exports = router;
