/**
 * TOTP/HOTP 模块 API 路由
 */

const express = require('express');
const router = express.Router();
const storage = require('./storage');
const totpService = require('./totp-service');
const { createLogger } = require('../../src/utils/logger');
const auditService = require('../../src/services/audit-service');
const eventBus = require('../../src/services/toolbox-event-bus');
const { encryptJson, decryptJson } = require('../../src/utils/secure-storage');

const logger = createLogger('TOTP');
const BACKUP_VERSION = 1;

function buildBackupPayload() {
  const accounts = storage.loadAccounts().map(acc => ({
    id: acc.id,
    otp_type: acc.otp_type,
    issuer: acc.issuer,
    account: acc.account,
    secret: acc.secret,
    algorithm: acc.algorithm,
    digits: acc.digits,
    period: acc.period,
    counter: acc.counter,
    group_id: acc.group_id,
    icon: acc.icon,
    color: acc.color,
    sort_order: acc.sort_order,
    created_at: acc.created_at,
  }));
  const groups = storage.loadGroups().map(group => ({
    id: group.id,
    name: group.name,
    icon: group.icon,
    color: group.color,
    sort_order: group.sort_order,
    created_at: group.created_at,
  }));
  return {
    type: 'api-monitor-totp-backup',
    version: BACKUP_VERSION,
    exportedAt: new Date().toISOString(),
    accounts,
    groups,
  };
}

function decodeBackupPayload(value) {
  if (!value) return null;
  if (typeof value === 'object') return value;
  const decoded = decryptJson(value, null);
  if (!decoded || decoded.type !== 'api-monitor-totp-backup') {
    throw new Error('Invalid encrypted TOTP backup');
  }
  return decoded;
}

function extractImportItems(body = {}) {
  const items = [];
  let backup = null;

  if (body.backup || body.payload) {
    backup = decodeBackupPayload(body.backup || body.payload);
    if (Array.isArray(backup.accounts)) {
      items.push(...backup.accounts);
    }
  }

  if (Array.isArray(body.uris)) {
    body.uris.forEach(uri => {
      const parsed = totpService.parseUri(uri);
      if (parsed) items.push(parsed);
    });
  }

  if (Array.isArray(body.accounts)) {
    items.push(...body.accounts);
  }

  return { items, backup };
}

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
 * GET /accounts/:id
 * 获取单个账号详情
 * 支持 ?showSecret=true 显示密钥
 */
router.get('/accounts/:id', async (req, res) => {
  try {
    const { id } = req.params;
    const account = storage.getAccount(id);

    if (!account) {
      return res.status(404).json({ success: false, error: '账号不存在' });
    }

    const showSecret = req.query.showSecret === 'true';
    if (showSecret) {
      storage.updateAccount(id, { last_revealed_at: new Date().toISOString() });
      auditService.record({
        req,
        module: 'totp',
        action: 'secret.revealed',
        resourceType: 'totp_account',
        resourceId: id,
        summary: `TOTP secret revealed for ${account.issuer}`,
        metadata: { issuer: account.issuer, account: account.account },
      });
      eventBus.publish(
        'totp.security.revealed',
        { accountId: id, issuer: account.issuer },
        { module: 'totp', severity: 'warning' }
      );
    }

    res.json({
      success: true,
      data: {
        ...account,
        secret: showSecret ? account.secret : undefined,
        hasSecret: !!account.secret
      }
    });
  } catch (error) {
    logger.error('获取账号详情失败', error.message);
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
    auditService.record({
      req,
      module: 'totp',
      action: 'created',
      resourceType: 'totp_account',
      resourceId: newAccount.id,
      summary: `Created TOTP account ${newAccount.issuer}`,
      metadata: { issuer: newAccount.issuer, account: newAccount.account },
    });
    eventBus.publish('totp.resource.created', { accountId: newAccount.id }, { module: 'totp' });

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
    auditService.record({
      req,
      module: 'totp',
      action: 'updated',
      resourceType: 'totp_account',
      resourceId: id,
      summary: `Updated TOTP account ${existing.issuer}`,
      metadata: { fields: Object.keys(updates) },
    });
    eventBus.publish(
      'totp.resource.updated',
      { accountId: id, fields: Object.keys(updates) },
      { module: 'totp' }
    );

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
    auditService.record({
      req,
      module: 'totp',
      action: 'deleted',
      resourceType: 'totp_account',
      resourceId: id,
      summary: `Deleted TOTP account ${existing.issuer}`,
      metadata: { issuer: existing.issuer, account: existing.account },
    });
    eventBus.publish('totp.resource.deleted', { accountId: id }, { module: 'totp' });
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

async function exportTotpBackup(req, res) {
  try {
    const payload = buildBackupPayload();

    if (req.query.format === 'uri' || req.query.plaintext === 'true') {
      const uris = payload.accounts.map(acc => totpService.generateUri(acc));
      auditService.record({
        req,
        module: 'totp',
        action: 'exported_plaintext',
        resourceType: 'totp_backup',
        summary: `Exported ${uris.length} plaintext TOTP URIs`,
      });
      eventBus.publish(
        'totp.backup.exported',
        { accountCount: uris.length, format: 'uri' },
        { module: 'totp', severity: 'warning' }
      );
      return res.json({ success: true, format: 'uri', data: uris });
    }

    const encrypted = encryptJson(payload);
    auditService.record({
      req,
      module: 'totp',
      action: 'exported',
      resourceType: 'totp_backup',
      summary: `Exported encrypted TOTP backup (${payload.accounts.length} accounts)`,
      metadata: { accountCount: payload.accounts.length, groupCount: payload.groups.length },
    });
    eventBus.publish(
      'totp.backup.exported',
      { accountCount: payload.accounts.length, groupCount: payload.groups.length, format: 'encrypted-backup' },
      { module: 'totp', severity: 'warning' }
    );
    res.json({
      success: true,
      format: 'encrypted-backup',
      data: {
        version: BACKUP_VERSION,
        exportedAt: payload.exportedAt,
        accountCount: payload.accounts.length,
        groupCount: payload.groups.length,
        payload: encrypted,
      },
    });
  } catch (error) {
    logger.error('导出失败', error.message);
    res.status(500).json({ success: false, error: error.message });
  }
}

/**
 * GET /export
 * 导出所有账号的 OTP URI
 */
router.get('/export', exportTotpBackup);
router.post('/export', exportTotpBackup);

/**
 * POST /import/preview
 * 预览导入结果，标记重复项和解析错误
 */
router.post('/import/preview', async (req, res) => {
  try {
    const existing = storage.loadAccounts();
    const existingKeys = new Set(
      existing.map(acc => `${String(acc.issuer || '').toLowerCase()}|${String(acc.account || '').toLowerCase()}`)
    );
    const { items, backup } = extractImportItems(req.body || {});
    const errors = [];

    const seen = new Set();
    const preview = items.map((item, index) => {
      const key = `${String(item.issuer || '').toLowerCase()}|${String(item.account || '').toLowerCase()}`;
      const duplicateExisting = existingKeys.has(key);
      const duplicateInBatch = seen.has(key);
      seen.add(key);
      return {
        index,
        issuer: item.issuer || '未知',
        account: item.account || '',
        otp_type: item.otp_type || 'totp',
        valid: !!item.secret,
        duplicate: duplicateExisting || duplicateInBatch,
        duplicateExisting,
        duplicateInBatch,
        error: item.secret ? null : '缺少密钥',
      };
    });

    res.json({
      success: true,
      data: {
        total: preview.length,
        valid: preview.filter(item => item.valid && !item.duplicate).length,
        duplicates: preview.filter(item => item.duplicate).length,
        errors,
        items: preview,
        backup: backup
          ? {
              version: backup.version,
              exportedAt: backup.exportedAt,
              groupCount: Array.isArray(backup.groups) ? backup.groups.length : 0,
            }
          : null,
      },
    });
  } catch (error) {
    logger.error('导入预览失败', error.message);
    res.status(500).json({ success: false, error: error.message });
  }
});

async function importTotpBackup(req, res) {
  try {
    const { items: toImport, backup } = extractImportItems(req.body || {});

    if (toImport.length === 0) {
      return res.status(400).json({ success: false, error: '没有有效的导入数据' });
    }

    const results = backup
      ? storage.importBackup({ ...backup, accounts: toImport })
      : storage.importAccounts(toImport);
    auditService.record({
      req,
      module: 'totp',
      action: 'imported',
      resourceType: 'totp_backup',
      summary: `Imported TOTP data: ${results.success} succeeded, ${results.failed} failed`,
      metadata: { success: results.success, failed: results.failed, backup: !!backup },
    });
    eventBus.publish(
      'totp.backup.imported',
      { success: results.success, failed: results.failed, backup: !!backup },
      { module: 'totp', severity: results.failed > 0 ? 'warning' : 'info' }
    );
    logger.info(`导入账号: 成功 ${results.success}, 失败 ${results.failed}`);
    res.json({ success: true, data: results });
  } catch (error) {
    logger.error('导入失败', error.message);
    res.status(500).json({ success: false, error: error.message });
  }
}

/**
 * POST /import
 */
router.post('/import', importTotpBackup);
router.post('/import/commit', importTotpBackup);

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

    const pluginDir = path.resolve(__dirname, '../../plugin');
    const tempDir = path.resolve(__dirname, '../../tmp');
    const zipFile = path.join(tempDir, 'api-monitor-2fa-extension.zip');

    if (!fs.existsSync(tempDir)) {
      fs.mkdirSync(tempDir, { recursive: true });
    }

    // 使用 PowerShell 进行压缩，确保路径处理更稳健
    const cmd = `powershell -Command "Compress-Archive -Path '${pluginDir}\\*' -DestinationPath '${zipFile}' -Force"`;

    logger.info(`正在压缩扩展程序: ${pluginDir} -> ${zipFile}`);

    exec(cmd, (error, stdout, stderr) => {
      if (error) {
        logger.error('压缩扩展失败', {
          message: error.message,
          stderr: stderr,
          stdout: stdout
        });
        return res.status(500).json({ success: false, error: '压缩失败: ' + (stderr || error.message) });
      }

      if (!fs.existsSync(zipFile)) {
        logger.error('压缩成功但未找到 ZIP 文件', zipFile);
        return res.status(500).json({ success: false, error: '文件生成失败' });
      }

      res.download(zipFile, 'api-monitor-2fa-extension.zip', err => {
        if (err) {
          logger.error('发送扩展失败', err.message);
        }
        // 发送后尝试删除临时文件
        setTimeout(() => {
          try {
            if (fs.existsSync(zipFile)) fs.unlinkSync(zipFile);
          } catch (e) { }
        }, 1000);
      });
    });
  } catch (error) {
    logger.error('下载扩展异常', error.message);
    res.status(500).json({ success: false, error: error.message });
  }
});

module.exports = router;
