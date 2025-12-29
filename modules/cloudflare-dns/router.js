/**
 * Cloudflare DNS 绠＄悊 - API 璺�敱
 */

const express = require('express');
const router = express.Router();
const storage = require('./storage');
const cfApi = require('./cloudflare-api');
const { createLogger } = require('../../src/utils/logger');

const logger = createLogger('CF-DNS');

// ==================== 璐﹀彿绠＄悊 ====================

/**
 * 鑾峰彇鎵€鏈夎处鍙凤紙闅愯棌 API Token锛?
 */
router.get('/accounts', (req, res) => {
  try {
    const accounts = storage.getAccounts();
    // 闅愯棌鏁忔劅淇℃伅
    const safeAccounts = accounts.map(a => ({
      id: a.id,
      name: a.name,
      email: a.email,
      createdAt: a.createdAt,
      lastUsed: a.lastUsed,
      hasToken: !!a.apiToken
    }));
    res.json(safeAccounts);
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 瀵煎嚭鎵€鏈夎处鍙凤紙鍖呭惈 API Token锛岀敤浜庡�浠斤級
 */
router.get('/accounts/export', (req, res) => {
  try {
    const accounts = storage.getAccounts();
    // 杩斿洖瀹屾暣淇℃伅鐢ㄤ簬瀵煎嚭
    const exportAccounts = accounts.map(a => ({
      name: a.name,
      email: a.email,
      apiToken: a.apiToken
    }));
    res.json({
      success: true,
      accounts: exportAccounts
    });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 娣诲姞璐﹀彿
 */
router.post('/accounts', async (req, res) => {
  try {
    const { name, apiToken, email, skipVerify } = req.body;

    if (!name || !apiToken) {
      return res.status(400).json({ error: '名称和 API Token 必填' });
    }

    // 楠岃瘉 Token锛堥櫎闈炴槑纭�烦杩囬獙璇侊紝鐢ㄤ簬鏁版嵁瀵煎叆锛?
    if (!skipVerify) {
      // 鏍规嵁鏄�惁鏈?email 閫夋嫨楠岃瘉鏂瑰紡
      const auth = email
        ? { email, key: apiToken }  // Global API Key
        : apiToken;  // API Token

      const verification = await cfApi.verifyToken(auth);
      if (!verification.valid) {
        return res.status(400).json({ error: `Token 鏃犳晥: ${verification.error}` });
      }
    }

    const account = storage.addAccount({ name, apiToken, email });
    res.json({
      success: true,
      account: {
        id: account.id,
        name: account.name,
        email: account.email,
        createdAt: account.createdAt
      }
    });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 鏇存柊璐﹀彿
 */
router.put('/accounts/:id', async (req, res) => {
  try {
    const { id } = req.params;
    const { name, apiToken, email } = req.body;

    // 濡傛灉鏇存柊 Token锛屽厛楠岃瘉
    if (apiToken) {
      // 鏍规嵁鏄�惁鏈?email 閫夋嫨楠岃瘉鏂瑰紡
      const auth = email
        ? { email, key: apiToken }  // Global API Key
        : apiToken;  // API Token

      const verification = await cfApi.verifyToken(auth);
      if (!verification.valid) {
        return res.status(400).json({ error: `Token 鏃犳晥: ${verification.error}` });
      }
    }

    const updated = storage.updateAccount(id, { name, apiToken, email });
    if (!updated) {
      return res.status(404).json({
        error: '账号不存在'
      });
    }

    res.json({ success: true });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 鍒犻櫎璐﹀彿
 */
router.delete('/accounts/:id', (req, res) => {
  try {
    const { id } = req.params;
    const deleted = storage.deleteAccount(id);
    if (!deleted) {
      return res.status(404).json({
        error: '账号不存在'
      });
    }
    res.json({ success: true });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 楠岃瘉璐﹀彿 Token
 */
router.post('/accounts/:id/verify', async (req, res) => {
  try {
    const { id } = req.params;
    const account = storage.getAccountById(id);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    // 鏍规嵁璐﹀彿閰嶇疆閫夋嫨璁よ瘉鏂瑰紡
    const auth = account.email
      ? { email: account.email, key: account.apiToken }  // Global API Key
      : account.apiToken;  // API Token

    const verification = await cfApi.verifyToken(auth);
    res.json(verification);
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 鑾峰彇璐﹀彿鐨?API Token锛堢敤浜庢樉绀猴級
 */
router.get('/accounts/:id/token', (req, res) => {
  try {
    const { id } = req.params;
    const account = storage.getAccountById(id);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    res.json({
      success: true,
      apiToken: account.apiToken
    });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

// ==================== Zone 绠＄悊 ====================

/**
 * 鑾峰彇璐﹀彿涓嬬殑鎵€鏈夊煙鍚?
 */
router.get('/accounts/:id/zones', async (req, res) => {
  try {
    const { id } = req.params;
    const account = storage.getAccountById(id);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    storage.touchAccount(id);

    // 鏍规嵁璐﹀彿閰嶇疆閫夋嫨璁よ瘉鏂瑰紡
    const auth = account.email
      ? { email: account.email, key: account.apiToken }
      : account.apiToken;

    const { zones, resultInfo } = await cfApi.listZones(auth);

    res.json({
      zones: zones.map(z => ({
        id: z.id,
        name: z.name,
        status: z.status,
        paused: z.paused,
        type: z.type,
        nameServers: z.name_servers,
        createdOn: z.created_on,
        modifiedOn: z.modified_on
      })),
      pagination: resultInfo
    });
  } catch (e) {
    logger.error(`获取域名列表失败 (ID: ${req.params.id}):`, e.message);
    res.status(500).json({ error: e.message });
  }
});

/**
 * 获取所有账号下的所有域名汇总 (用于仪表盘)
 */
router.get('/zones', async (req, res) => {
  try {
    const accounts = storage.getAccounts();
    const allZones = [];

    // 并发请求所有账号的域名
    await Promise.all(accounts.map(async (account) => {
      try {
        const auth = account.email ? { email: account.email, key: account.apiToken } : account.apiToken;
        const { zones } = await cfApi.listZones(auth);
        if (zones && Array.isArray(zones)) {
          allZones.push(...zones);
        }
      } catch (err) {
        logger.error(`汇总账号 ${account.name} 域名失败:`, err.message);
      }
    }));

    res.json({
      success: true,
      data: allZones
    });
  } catch (e) {
    res.status(500).json({ success: false, error: e.message });
  }
});

/**
 * 鍒涘缓鍩熷悕 (娣诲姞鏂?Zone)
 */
router.post('/accounts/:id/zones', async (req, res) => {
  try {
    const { id } = req.params;
    const { name, jumpStart } = req.body;

    if (!name) {
      return res.status(400).json({ error: '域名不能为空' });
    }

    const account = storage.getAccountById(id);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    storage.touchAccount(id);

    // 鑾峰彇 Cloudflare Account ID
    const auth = account.email ? { email: account.email, key: account.apiToken } : account.apiToken; const cfAccountId = await cfApi.getAccountId(auth);

    // 鍒涘缓鏂板煙鍚?
    const zone = await cfApi.createZone(auth, name, {
      account: { id: cfAccountId },
      jump_start: jumpStart !== undefined ? jumpStart : false
    });

    logger.info(`鍩熷悕鍒涘缓鎴愬姛: ${name} (Zone ID: ${zone.id})`);

    res.json({
      success: true,
      zone: {
        id: zone.id,
        name: zone.name,
        status: zone.status,
        nameServers: zone.name_servers,
        createdOn: zone.created_on
      }
    });
  } catch (e) {
    logger.error(`鍒涘缓鍩熷悕澶辫触:`, e.message);
    res.status(500).json({ error: e.message });
  }
});

/**
 * 鍒犻櫎鍩熷悕 (鍒犻櫎 Zone)
 */
router.delete('/accounts/:accountId/zones/:zoneId', async (req, res) => {
  try {
    const { accountId, zoneId } = req.params;

    const account = storage.getAccountById(accountId);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    storage.touchAccount(accountId);
    const result = await cfApi.deleteZone(auth, zoneId);

    logger.info(`鍩熷悕鍒犻櫎鎴愬姛: Zone ID ${zoneId}`);

    res.json({
      success: true,
      result
    });
  } catch (e) {
    logger.error(`鍒犻櫎鍩熷悕澶辫触:`, e.message);
    res.status(500).json({ error: e.message });
  }
});

// ==================== DNS 璁板綍绠＄悊 ====================

/**
 * 鑾峰彇鍩熷悕鐨?DNS 璁板綍
 */
router.get('/accounts/:accountId/zones/:zoneId/records', async (req, res) => {
  try {
    const { accountId, zoneId } = req.params;
    const { type, name, page } = req.query;

    const account = storage.getAccountById(accountId);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    storage.touchAccount(accountId);

    // 鏍规嵁璐﹀彿閰嶇疆閫夋嫨璁よ瘉鏂瑰紡
    const auth = account.email
      ? { email: account.email, key: account.apiToken }
      : account.apiToken;

    const { records, resultInfo } = await cfApi.listDnsRecords(
      auth,
      zoneId,
      { type, name, page }
    );

    res.json({
      records: records.map(r => ({
        id: r.id,
        type: r.type,
        name: r.name,
        content: r.content,
        proxied: r.proxied,
        ttl: r.ttl,
        priority: r.priority,
        createdOn: r.created_on,
        modifiedOn: r.modified_on
      })),
      pagination: resultInfo
    });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 鍒涘缓 DNS 璁板綍
 */
router.post('/accounts/:accountId/zones/:zoneId/records', async (req, res) => {
  try {
    const { accountId, zoneId } = req.params;
    const { type, name, content, ttl, proxied, priority } = req.body;

    const account = storage.getAccountById(accountId);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    // 楠岃瘉璁板綍
    const validation = cfApi.validateDnsRecord({ type, name, content, priority });
    if (!validation.valid) {
      return res.status(400).json({ error: validation.errors.join(', ') });
    }

    storage.touchAccount(accountId);

    // 根据账号配置选择认证方式
    const auth = account.email
      ? { email: account.email, key: account.apiToken }  // Global API Key
      : account.apiToken;  // API Token

    const record = await cfApi.createDnsRecord(
      auth,
      zoneId,
      { type, name, content, ttl, proxied, priority }
    );

    res.json({
      success: true,
      record: {
        id: record.id,
        type: record.type,
        name: record.name,
        content: record.content,
        proxied: record.proxied,
        ttl: record.ttl
      }
    });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 鏇存柊 DNS 璁板綍
 */
router.put('/accounts/:accountId/zones/:zoneId/records/:recordId', async (req, res) => {
  try {
    const { accountId, zoneId, recordId } = req.params;
    const { type, name, content, ttl, proxied, priority } = req.body;

    const account = storage.getAccountById(accountId);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    storage.touchAccount(accountId);

    // 根据账号配置选择认证方式
    const auth = account.email
      ? { email: account.email, key: account.apiToken }  // Global API Key
      : account.apiToken;  // API Token

    const record = await cfApi.updateDnsRecord(
      auth,
      zoneId,
      recordId,
      { type, name, content, ttl, proxied, priority }
    );

    res.json({
      success: true,
      record: {
        id: record.id,
        type: record.type,
        name: record.name,
        content: record.content,
        proxied: record.proxied,
        ttl: record.ttl
      }
    });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 鍒犻櫎 DNS 璁板綍
 */
router.delete('/accounts/:accountId/zones/:zoneId/records/:recordId', async (req, res) => {
  try {
    const { accountId, zoneId, recordId } = req.params;

    const account = storage.getAccountById(accountId);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    storage.touchAccount(accountId);

    // 根据账号配置选择认证方式
    const auth = account.email
      ? { email: account.email, key: account.apiToken }  // Global API Key
      : account.apiToken;  // API Token

    await cfApi.deleteDnsRecord(auth, zoneId, recordId);

    res.json({ success: true });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 蹇�€熷垏鎹?DNS 璁板綍鍐呭�
 */
router.post('/accounts/:accountId/zones/:zoneId/switch', async (req, res) => {
  try {
    const { accountId, zoneId } = req.params;
    const { type, name, newContent } = req.body;

    if (!type || !name || !newContent) {
      return res.status(400).json({ error: 'type, name, newContent 蹇呭～' });
    }

    const account = storage.getAccountById(accountId);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    storage.touchAccount(accountId);
    const updated = await cfApi.switchDnsContent(
      account.apiToken,
      zoneId,
      type,
      name,
      newContent
    );

    res.json({
      success: true,
      updated: updated.length,
      records: updated.map(r => ({
        id: r.id,
        name: r.name,
        content: r.content
      }))
    });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 鎵归噺鍒涘缓 DNS 璁板綍
 */
router.post('/accounts/:accountId/zones/:zoneId/batch', async (req, res) => {
  try {
    const { accountId, zoneId } = req.params;
    const { records } = req.body;

    if (!records || !Array.isArray(records)) {
      return res.status(400).json({ error: '闇€瑕佹彁渚?records 鏁扮粍' });
    }

    const account = storage.getAccountById(accountId);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    storage.touchAccount(accountId);
    const { results, errors } = await cfApi.batchCreateDnsRecords(
      account.apiToken,
      zoneId,
      records
    );

    res.json({
      success: errors.length === 0,
      created: results.length,
      failed: errors.length,
      results,
      errors
    });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

// ==================== 缂撳瓨绠＄悊 ====================

/**
 * 娓呴櫎鍩熷悕鐨勬墍鏈夌紦瀛?
 */
router.post('/accounts/:accountId/zones/:zoneId/purge', async (req, res) => {
  try {
    const { accountId, zoneId } = req.params;
    const { purge_everything } = req.body;

    logger.info(`鏀跺埌娓呴櫎缂撳瓨璇锋眰 - Account: ${accountId}, Zone: ${zoneId}, Body:`, req.body);

    const account = storage.getAccountById(accountId);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    storage.touchAccount(accountId);

    // 鏍规嵁璐﹀彿閰嶇疆閫夋嫨璁よ瘉鏂瑰紡
    const auth = account.email
      ? { email: account.email, key: account.apiToken }  // Global API Key
      : account.apiToken;  // API Token

    logger.info(`浣跨敤璁よ瘉鏂瑰紡: ${account.email ? 'Global API Key' : 'API Token'}`);

    // 璋冪敤 Cloudflare API 娓呴櫎缂撳瓨
    logger.info(`璋冪敤 Cloudflare API 娓呴櫎缂撳瓨...`);
    const result = await cfApi.purgeCache(auth, zoneId, { purge_everything });

    logger.info(`缂撳瓨宸叉竻闄ゆ垚鍔?(Zone: ${zoneId})`);

    res.json({
      success: true,
      message: '缓存已清除',
      result
    });
  } catch (e) {
    logger.error(`娓呴櫎缂撳瓨澶辫触:`, e.message, e.stack);
    res.status(500).json({ error: e.message, details: e.stack });
  }
});

// ==================== SSL/TLS 绠＄悊 ====================

/**
 * 鑾峰彇鍩熷悕鐨凷SL/TLS淇℃伅
 */
router.get('/accounts/:accountId/zones/:zoneId/ssl', async (req, res) => {
  try {
    const { accountId, zoneId } = req.params;

    const account = storage.getAccountById(accountId);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    storage.touchAccount(accountId);

    // 璁よ瘉鏂瑰紡閫夋嫨
    const auth = account.email
      ? { email: account.email, key: account.apiToken }
      : account.apiToken;

    // 骞惰�鑾峰彇澶氫釜SSL鐩稿叧淇℃伅
    const [settings, certificates, verification] = await Promise.all([
      cfApi.getSslSettings(auth, zoneId),
      cfApi.getSslCertificates(auth, zoneId),
      cfApi.getSslVerification(auth, zoneId)
    ]);

    logger.info(`鑾峰彇SSL淇℃伅鎴愬姛 (Zone: ${zoneId})`);

    res.json({
      success: true,
      ssl: {
        mode: settings.value,
        modifiedOn: settings.modified_on,
        editable: settings.editable,
        certificates: certificates.map(cert => ({
          id: cert.id,
          type: cert.type,
          hosts: cert.hosts,
          status: cert.status,
          validityDays: cert.validity_days,
          certificateAuthority: cert.certificate_authority,
          primary: cert.primary
        })),
        verification: verification
      }
    });
  } catch (e) {
    logger.error(`鑾峰彇SSL淇℃伅澶辫触:`, e.message);
    res.status(500).json({ error: e.message });
  }
});

/**
 * 淇�敼鍩熷悕鐨凷SL妯″紡
 */
router.patch('/accounts/:accountId/zones/:zoneId/ssl', async (req, res) => {
  try {
    const { accountId, zoneId } = req.params;
    const { mode } = req.body;

    if (!mode || !['off', 'flexible', 'full', 'strict'].includes(mode)) {
      return res.status(400).json({ error: '鏃犳晥鐨凷SL妯″紡' });
    }

    const account = storage.getAccountById(accountId);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    storage.touchAccount(accountId);

    const auth = account.email
      ? { email: account.email, key: account.apiToken }
      : account.apiToken;

    const result = await cfApi.updateSslMode(auth, zoneId, mode);

    logger.info(`SSL妯″紡宸叉洿鏂?(Zone: ${zoneId}, Mode: ${mode})`);

    res.json({
      success: true,
      ssl: {
        mode: result.value,
        modifiedOn: result.modified_on
      }
    });
  } catch (e) {
    logger.error(`鏇存柊SSL妯″紡澶辫触:`, e.message);
    res.status(500).json({ error: e.message });
  }
});

// ==================== Analytics 鍒嗘瀽 ====================

/**
 * 鑾峰彇鍩熷悕鐨凙nalytics鏁版嵁
 */
router.get('/accounts/:accountId/zones/:zoneId/analytics', async (req, res) => {
  try {
    const { accountId, zoneId } = req.params;
    const { timeRange = '24h' } = req.query;

    const account = storage.getAccountById(accountId);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    storage.touchAccount(accountId);

    const auth = account.email
      ? { email: account.email, key: account.apiToken }
      : account.apiToken;

    const analytics = await cfApi.getSimpleAnalytics(auth, zoneId, timeRange);

    logger.info(`鑾峰彇Analytics鎴愬姛 (Zone: ${zoneId}, Range: ${timeRange})`);

    res.json({
      success: true,
      analytics,
      timeRange
    });
  } catch (e) {
    logger.error(`鑾峰彇Analytics澶辫触:`, e.message);
    res.status(500).json({ error: e.message });
  }
});

// ==================== DNS 妯℃澘绠＄悊 ====================

/**
 * 鑾峰彇鎵€鏈夋ā鏉?
 */
router.get('/templates', (req, res) => {
  try {
    const templates = storage.getTemplates();
    res.json(templates);
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 娣诲姞妯℃澘
 */
router.post('/templates', (req, res) => {
  try {
    const { name, type, content, proxied, ttl, priority, description } = req.body;

    if (!name || !type || !content) {
      return res.status(400).json({
        error: '名称、类型、内容必填'
      });
    }

    const template = storage.addTemplate({
      name, type, content, proxied, ttl, priority, description
    });

    res.json({ success: true, template });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 鏇存柊妯℃澘
 */
router.put('/templates/:id', (req, res) => {
  try {
    const { id } = req.params;
    const updated = storage.updateTemplate(id, req.body);

    if (!updated) {
      return res.status(404).json({
        error: '模板不存在'
      });
    }

    res.json({ success: true, template: updated });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 鍒犻櫎妯℃澘
 */
router.delete('/templates/:id', (req, res) => {
  try {
    const { id } = req.params;
    const deleted = storage.deleteTemplate(id);

    if (!deleted) {
      return res.status(404).json({
        error: '模板不存在'
      });
    }

    res.json({ success: true });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 搴旂敤妯℃澘鍒板煙鍚?
 */
router.post('/templates/:templateId/apply', async (req, res) => {
  try {
    const { templateId } = req.params;
    const { accountId, zoneId, recordName } = req.body;

    if (!accountId || !zoneId || !recordName) {
      return res.status(400).json({ error: 'accountId, zoneId, recordName 蹇呭～' });
    }

    const account = storage.getAccountById(accountId);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    const templates = storage.getTemplates();
    const template = templates.find(t => t.id === templateId);
    if (!template) {
      return res.status(404).json({
        error: '模板不存在'
      });
    }

    storage.touchAccount(accountId);
    const record = await cfApi.createDnsRecord(
      account.apiToken,
      zoneId,
      {
        type: template.type,
        name: recordName,
        content: template.content,
        ttl: template.ttl,
        proxied: template.proxied,
        priority: template.priority
      }
    );

    res.json({
      success: true,
      record: {
        id: record.id,
        type: record.type,
        name: record.name,
        content: record.content
      }
    });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

// ==================== 瀹炵敤鍔熻兘 ====================

/**
 * 鑾峰彇鏀�寔鐨勮�褰曠被鍨?
 */
router.get('/record-types', (req, res) => {
  res.json(cfApi.getSupportedRecordTypes());
});

/**
 * 瀵煎嚭璐﹀彿锛堝寘鍚�畬鏁存暟鎹�紝鐢ㄤ簬澶囦唤锛?
 */
router.get('/export/accounts', (req, res) => {
  try {
    const accounts = storage.getAccounts();
    res.json(accounts);
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 鎵归噺瀵煎叆璐﹀彿锛堢洿鎺ヨ�鐩栨暟鎹�簱锛?
 */
router.post('/import/accounts', (req, res) => {
  try {
    const { accounts, overwrite } = req.body;

    if (!accounts || !Array.isArray(accounts)) {
      return res.status(400).json({ error: '闇€瑕佹彁渚?accounts 鏁扮粍' });
    }

    if (overwrite) {
      // 鐩存帴瑕嗙洊鎵€鏈夎处鍙?
      storage.saveAccounts(accounts);
    } else {
      // 杩藉姞璐﹀彿
      accounts.forEach(account => {
        storage.addAccount(account);
      });
    }

    res.json({ success: true, count: accounts.length });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 鎵归噺瀵煎叆妯℃澘锛堢洿鎺ヨ�鐩栨暟鎹�簱锛?
 */
router.post('/import/templates', (req, res) => {
  try {
    const { templates, overwrite } = req.body;

    if (!templates || !Array.isArray(templates)) {
      return res.status(400).json({ error: '闇€瑕佹彁渚?templates 鏁扮粍' });
    }

    if (overwrite) {
      // 鐩存帴瑕嗙洊鎵€鏈夋ā鏉?
      storage.saveTemplates(templates);
    } else {
      // 杩藉姞妯℃澘
      templates.forEach(template => {
        storage.addTemplate(template);
      });
    }

    res.json({ success: true, count: templates.length });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

// ==================== Workers 绠＄悊 ====================

/**
 * 鑾峰彇璐﹀彿鐨?Cloudflare Account ID
 */
router.get('/accounts/:id/cf-account-id', async (req, res) => {
  try {
    const { id } = req.params;
    const account = storage.getAccountById(id);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    const auth = account.email ? { email: account.email, key: account.apiToken } : account.apiToken; const cfAccountId = await cfApi.getAccountId(auth);
    res.json({ success: true, cfAccountId });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 鑾峰彇璐﹀彿涓嬬殑鎵€鏈?Workers
 */
router.get('/accounts/:id/workers', async (req, res) => {
  try {
    const { id } = req.params;
    const account = storage.getAccountById(id);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    storage.touchAccount(id);

    // 鍏堣幏鍙?CF Account ID
    const auth = account.email ? { email: account.email, key: account.apiToken } : account.apiToken;
    const cfAccountId = await cfApi.getAccountId(auth);
    const workers = await cfApi.listWorkers(auth, cfAccountId);
    logger.info(`鑾峰彇鍒?${workers.length} 涓?Workers (Account: ${cfAccountId})`);

    // 鑾峰彇瀛愬煙鍚嶄俊鎭?
    const subdomain = await cfApi.getWorkersSubdomain(auth, cfAccountId);

    res.json({
      workers: workers.map(w => ({
        id: w.id,
        name: w.id, // Worker 鐨?id 灏辨槸鍚嶇О
        createdOn: w.created_on,
        modifiedOn: w.modified_on,
        etag: w.etag
      })),
      subdomain: subdomain?.subdomain || null,
      cfAccountId
    });
  } catch (e) {
    logger.error(`鑾峰彇 Workers 鍒楄〃澶辫触:`, e.message);
    res.status(500).json({ error: e.message });
  }
});

/**
 * 鑾峰彇 Worker 鑴氭湰鍐呭�
 */
router.get('/accounts/:id/workers/:scriptName', async (req, res) => {
  try {
    const { id, scriptName } = req.params;
    const account = storage.getAccountById(id);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    const auth = account.email ? { email: account.email, key: account.apiToken } : account.apiToken; const cfAccountId = await cfApi.getAccountId(auth);
    const worker = await cfApi.getWorkerScript(auth, cfAccountId, scriptName);

    res.json({
      success: true,
      worker: {
        name: worker.name,
        script: worker.script,
        meta: worker.meta
      }
    });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 鍒涘缓鎴栨洿鏂?Worker 鑴氭湰
 */
router.put('/accounts/:id/workers/:scriptName', async (req, res) => {
  try {
    const { id, scriptName } = req.params;
    const { script, bindings, compatibility_date } = req.body;

    logger.info(`淇濆瓨 Worker: ${scriptName}, 鑴氭湰闀垮害: ${script?.length || 0}`);

    if (!script) {
      return res.status(400).json({ error: '鑴氭湰鍐呭�涓嶈兘涓虹┖' });
    }

    const account = storage.getAccountById(id);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    storage.touchAccount(id);
    const auth = account.email ? { email: account.email, key: account.apiToken } : account.apiToken; const cfAccountId = await cfApi.getAccountId(auth);
    logger.info(`CF Account ID: ${cfAccountId}`);

    const result = await cfApi.putWorkerScript(
      account.apiToken,
      cfAccountId,
      scriptName,
      script,
      { bindings, compatibility_date }
    );

    res.json({
      success: true,
      worker: result
    });
  } catch (e) {
    logger.error(`淇濆瓨 Worker 澶辫触:`, e.message, e.stack);
    res.status(500).json({ error: e.message });
  }
});

/**
 * 鍒犻櫎 Worker 鑴氭湰
 */
router.delete('/accounts/:id/workers/:scriptName', async (req, res) => {
  try {
    const { id, scriptName } = req.params;
    const account = storage.getAccountById(id);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    storage.touchAccount(id);
    const auth = account.email ? { email: account.email, key: account.apiToken } : account.apiToken; const cfAccountId = await cfApi.getAccountId(auth);
    await cfApi.deleteWorkerScript(auth, cfAccountId, scriptName);

    res.json({ success: true });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 鍚�敤/绂佺敤 Worker (瀛愬煙璁块棶)
 */
router.post('/accounts/:id/workers/:scriptName/toggle', async (req, res) => {
  try {
    const { id, scriptName } = req.params;
    const { enabled } = req.body;

    const account = storage.getAccountById(id);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    storage.touchAccount(id);
    const auth = account.email ? { email: account.email, key: account.apiToken } : account.apiToken; const cfAccountId = await cfApi.getAccountId(auth);
    const result = await cfApi.setWorkerEnabled(auth, cfAccountId, scriptName, enabled);

    res.json({ success: true, result });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 鑾峰彇鍩熷悕鐨?Worker 璺�敱
 */
router.get('/accounts/:accountId/zones/:zoneId/workers/routes', async (req, res) => {
  try {
    const { accountId, zoneId } = req.params;
    const account = storage.getAccountById(accountId);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    storage.touchAccount(accountId);
    const routes = await cfApi.listWorkerRoutes(auth, zoneId);

    res.json({
      routes: routes.map(r => ({
        id: r.id,
        pattern: r.pattern,
        script: r.script
      }))
    });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 鍒涘缓 Worker 璺�敱
 */
router.post('/accounts/:accountId/zones/:zoneId/workers/routes', async (req, res) => {
  try {
    const { accountId, zoneId } = req.params;
    const { pattern, script } = req.body;

    if (!pattern || !script) {
      return res.status(400).json({ error: 'pattern 鍜?script 蹇呭～' });
    }

    const account = storage.getAccountById(accountId);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    storage.touchAccount(accountId);
    const route = await cfApi.createWorkerRoute(auth, zoneId, pattern, script);

    res.json({
      success: true,
      route: {
        id: route.id,
        pattern: route.pattern,
        script: route.script
      }
    });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 鏇存柊 Worker 璺�敱
 */
router.put('/accounts/:accountId/zones/:zoneId/workers/routes/:routeId', async (req, res) => {
  try {
    const { accountId, zoneId, routeId } = req.params;
    const { pattern, script } = req.body;

    const account = storage.getAccountById(accountId);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    storage.touchAccount(accountId);
    const route = await cfApi.updateWorkerRoute(auth, zoneId, routeId, pattern, script);

    res.json({
      success: true,
      route: {
        id: route.id,
        pattern: route.pattern,
        script: route.script
      }
    });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 鍒犻櫎 Worker 璺�敱
 */
router.delete('/accounts/:accountId/zones/:zoneId/workers/routes/:routeId', async (req, res) => {
  try {
    const { accountId, zoneId, routeId } = req.params;
    const account = storage.getAccountById(accountId);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    storage.touchAccount(accountId);
    await cfApi.deleteWorkerRoute(auth, zoneId, routeId);

    res.json({ success: true });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 鑾峰彇 Worker 缁熻�淇℃伅
 */
router.get('/accounts/:id/workers/:scriptName/analytics', async (req, res) => {
  try {
    const { id, scriptName } = req.params;
    const { since } = req.query;

    const account = storage.getAccountById(id);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    const auth = account.email ? { email: account.email, key: account.apiToken } : account.apiToken; const cfAccountId = await cfApi.getAccountId(auth);
    const analytics = await cfApi.getWorkerAnalytics(auth, cfAccountId, scriptName, since);

    res.json({ success: true, analytics });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

// ==================== Workers 鑷�畾涔夊煙鍚嶇�鐞?====================

/**
 * 鑾峰彇 Worker 鐨勮嚜瀹氫箟鍩熷悕鍒楄〃
 */
router.get('/accounts/:id/workers/:scriptName/domains', async (req, res) => {
  try {
    const { id, scriptName } = req.params;
    const account = storage.getAccountById(id);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    const auth = account.email ? { email: account.email, key: account.apiToken } : account.apiToken; const cfAccountId = await cfApi.getAccountId(auth);
    const domains = await cfApi.listWorkerDomains(auth, cfAccountId, scriptName);

    res.json({
      success: true,
      domains: domains.map(d => ({
        id: d.id,
        hostname: d.hostname,
        service: d.service,
        environment: d.environment,
        zoneId: d.zone_id,
        zoneName: d.zone_name
      }))
    });
  } catch (e) {
    logger.error(`鑾峰彇 Worker 鍩熷悕澶辫触:`, e.message);
    res.status(500).json({ error: e.message });
  }
});

/**
 * 娣诲姞 Worker 鑷�畾涔夊煙鍚?
 */
router.post('/accounts/:id/workers/:scriptName/domains', async (req, res) => {
  try {
    const { id, scriptName } = req.params;
    const { hostname, environment } = req.body;

    if (!hostname) {
      return res.status(400).json({ error: '请输入域名' });
    }

    const account = storage.getAccountById(id);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    storage.touchAccount(id);
    const auth = account.email ? { email: account.email, key: account.apiToken } : account.apiToken;
    const cfAccountId = await cfApi.getAccountId(auth);
    const result = await cfApi.addWorkerDomain(auth, cfAccountId, scriptName, hostname, environment || 'production');

    res.json({ success: true, domain: result });
  } catch (e) {
    logger.error('添加 Worker 域名失败:', e.message);
    res.status(500).json({ error: e.message });
  }
});

/**
 * 鍒犻櫎 Worker 鑷�畾涔夊煙鍚?
 */
router.delete('/accounts/:id/workers/:scriptName/domains/:domainId', async (req, res) => {
  try {
    const { id, scriptName, domainId } = req.params;
    const account = storage.getAccountById(id);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    storage.touchAccount(id);
    const auth = account.email ? { email: account.email, key: account.apiToken } : account.apiToken; const cfAccountId = await cfApi.getAccountId(auth);
    await cfApi.deleteWorkerDomain(auth, cfAccountId, domainId);

    res.json({ success: true });
  } catch (e) {
    logger.error(`鍒犻櫎 Worker 鍩熷悕澶辫触:`, e.message);
    res.status(500).json({ error: e.message });
  }
});

// ==================== Pages 绠＄悊璺�敱 ====================


/**
 * 鑾峰彇 Pages 椤圭洰鍒楄〃
 */
router.get('/accounts/:id/pages', async (req, res) => {
  try {
    const { id } = req.params;
    const account = storage.getAccountById(id);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    storage.touchAccount(id);
    const auth = account.email ? { email: account.email, key: account.apiToken } : account.apiToken; const cfAccountId = await cfApi.getAccountId(auth);
    const projects = await cfApi.listPagesProjects(auth, cfAccountId);

    logger.info(`鑾峰彇鍒?${projects.length} 涓?Pages 椤圭洰 (Account: ${cfAccountId})`);

    res.json({
      projects: projects.map(p => ({
        name: p.name,
        subdomain: p.subdomain,
        domains: p.domains || [],
        createdOn: p.created_on,
        productionBranch: p.production_branch,
        latestDeployment: p.latest_deployment ? {
          id: p.latest_deployment.id,
          url: p.latest_deployment.url,
          status: p.latest_deployment.latest_stage?.status || 'unknown',
          createdOn: p.latest_deployment.created_on
        } : null
      })),
      cfAccountId
    });
  } catch (e) {
    logger.error(`鑾峰彇 Pages 椤圭洰澶辫触:`, e.message);
    res.status(500).json({ error: e.message });
  }
});

/**
 * 鑾峰彇 Pages 椤圭洰鐨勯儴缃插垪琛?
 */
router.get('/accounts/:id/pages/:projectName/deployments', async (req, res) => {
  try {
    const { id, projectName } = req.params;
    const account = storage.getAccountById(id);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    const auth = account.email ? { email: account.email, key: account.apiToken } : account.apiToken; const cfAccountId = await cfApi.getAccountId(auth);
    const deployments = await cfApi.listPagesDeployments(auth, cfAccountId, projectName);

    res.json({
      success: true,
      deployments: (deployments || []).map(d => {
        // 闃插尽鎬у�鐞嗭細闃叉� d 涓虹┖鎴栧瓧娈电己澶?
        if (!d) return null;
        return {
          id: d.id,
          url: d.url,
          environment: d.environment,
          status: (d.latest_stage && d.latest_stage.status) ? d.latest_stage.status : 'unknown',
          createdOn: d.created_on,
          source: d.source,
          buildConfig: d.build_config
        };
      }).filter(d => d !== null) // 杩囨护鎺夋棤鏁堥」
    });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 鍒犻櫎 Pages 閮ㄧ讲
 */
router.delete('/accounts/:id/pages/:projectName/deployments/:deploymentId', async (req, res) => {
  try {
    const { id, projectName, deploymentId } = req.params;
    const account = storage.getAccountById(id);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    const auth = account.email ? { email: account.email, key: account.apiToken } : account.apiToken; const cfAccountId = await cfApi.getAccountId(auth);
    await cfApi.deletePagesDeployment(auth, cfAccountId, projectName, deploymentId);

    res.json({ success: true });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 鑾峰彇 Pages 椤圭洰鐨勮嚜瀹氫箟鍩熷悕
 */
router.get('/accounts/:id/pages/:projectName/domains', async (req, res) => {
  try {
    const { id, projectName } = req.params;
    const account = storage.getAccountById(id);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    const auth = account.email ? { email: account.email, key: account.apiToken } : account.apiToken; const cfAccountId = await cfApi.getAccountId(auth);
    const domains = await cfApi.listPagesDomains(auth, cfAccountId, projectName);

    res.json({
      success: true,
      domains: domains.map(d => ({
        id: d.id,
        name: d.name,
        status: d.status,
        validationStatus: d.validation_data?.status || null,
        createdOn: d.created_on
      }))
    });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 娣诲姞 Pages 鑷�畾涔夊煙鍚?
 */
router.post('/accounts/:id/pages/:projectName/domains', async (req, res) => {
  try {
    const { id, projectName } = req.params;
    const { domain } = req.body;

    if (!domain) {
      return res.status(400).json({
        error: '请输入域名'
      });
    }

    const account = storage.getAccountById(id);
    if (!account) {
      return res.status(404).json({
        error: '账号不存在'
      });
    }

    const auth = account.email ? { email: account.email, key: account.apiToken } : account.apiToken; const cfAccountId = await cfApi.getAccountId(auth);
    const result = await cfApi.addPagesDomain(auth, cfAccountId, projectName, domain);

    res.json({ success: true, domain: result.result });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 鍒犻櫎 Pages 鑷�畾涔夊煙鍚?
 */
router.delete('/accounts/:id/pages/:projectName/domains/:domain', async (req, res) => {
  try {
    const { id, projectName, domain } = req.params;
    const account = storage.getAccountById(id);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    const auth = account.email ? { email: account.email, key: account.apiToken } : account.apiToken; const cfAccountId = await cfApi.getAccountId(auth);
    await cfApi.deletePagesDomain(auth, cfAccountId, projectName, domain);

    res.json({ success: true });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

/**
 * 鍒犻櫎 Pages 椤圭洰
 */
router.delete('/accounts/:id/pages/:projectName', async (req, res) => {
  try {
    const { id, projectName } = req.params;
    const account = storage.getAccountById(id);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    const auth = account.email ? { email: account.email, key: account.apiToken } : account.apiToken; const cfAccountId = await cfApi.getAccountId(auth);
    await cfApi.deletePagesProject(auth, cfAccountId, projectName);

    res.json({ success: true });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

// ==================== R2 存储管理 ====================

/**
 * 获取 R2 存储桶列表
 */
router.get('/accounts/:accountId/r2/buckets', async (req, res) => {
  try {
    const { accountId } = req.params;
    const account = storage.getAccountById(accountId);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    const auth = account.email ? { email: account.email, key: account.apiToken } : account.apiToken;
    const cfAccountId = await cfApi.getAccountId(auth);

    const buckets = await cfApi.listR2Buckets(auth, cfAccountId);
    res.json({ success: true, buckets });
  } catch (e) {
    logger.error('获取 R2 存储桶失败:', e.message);
    res.status(500).json({ error: e.message });
  }
});

/**
 * 创建 R2 存储桶
 */
router.post('/accounts/:accountId/r2/buckets', async (req, res) => {
  try {
    const { accountId } = req.params;
    const { name, location } = req.body;

    if (!name) {
      return res.status(400).json({ error: '桶名称必填' });
    }

    const account = storage.getAccountById(accountId);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    const auth = account.email ? { email: account.email, key: account.apiToken } : account.apiToken;
    const cfAccountId = await cfApi.getAccountId(auth);

    const bucket = await cfApi.createR2Bucket(auth, cfAccountId, name, location);
    res.json({ success: true, bucket });
  } catch (e) {
    logger.error('创建 R2 存储桶失败:', e.message);
    res.status(500).json({ error: e.message });
  }
});

/**
 * 删除 R2 存储桶
 */
router.delete('/accounts/:accountId/r2/buckets/:bucketName', async (req, res) => {
  try {
    const { accountId, bucketName } = req.params;

    const account = storage.getAccountById(accountId);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    const auth = account.email ? { email: account.email, key: account.apiToken } : account.apiToken;
    const cfAccountId = await cfApi.getAccountId(auth);

    await cfApi.deleteR2Bucket(auth, cfAccountId, bucketName);
    res.json({ success: true });
  } catch (e) {
    logger.error('删除 R2 存储桶失败:', e.message);
    res.status(500).json({ error: e.message });
  }
});

/**
 * 列出 R2 对象
 */
router.get('/accounts/:accountId/r2/buckets/:bucketName/objects', async (req, res) => {
  try {
    const { accountId, bucketName } = req.params;
    const { prefix, cursor, limit, delimiter } = req.query;

    const account = storage.getAccountById(accountId);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    const auth = account.email ? { email: account.email, key: account.apiToken } : account.apiToken;
    const cfAccountId = await cfApi.getAccountId(auth);

    const result = await cfApi.listR2Objects(auth, cfAccountId, bucketName, {
      prefix, cursor, limit, delimiter
    });

    res.json({ success: true, ...result });
  } catch (e) {
    logger.error('列出 R2 对象失败:', e.message);
    res.status(500).json({ error: e.message });
  }
});

/**
 * 删除 R2 对象
 */
router.delete('/accounts/:accountId/r2/buckets/:bucketName/objects/:objectKey', async (req, res) => {
  try {
    const { accountId, bucketName, objectKey } = req.params;

    const account = storage.getAccountById(accountId);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    const auth = account.email ? { email: account.email, key: account.apiToken } : account.apiToken;
    const cfAccountId = await cfApi.getAccountId(auth);

    await cfApi.deleteR2Object(auth, cfAccountId, bucketName, objectKey);
    res.json({ success: true });
  } catch (e) {
    logger.error('删除 R2 对象失败:', e.message);
    res.status(500).json({ error: e.message });
  }
});

/**
 * 获取 R2 对象下载 URL
 * 尝试获取存储桶的公开访问配置
 */
router.get('/accounts/:accountId/r2/buckets/:bucketName/objects/:objectKey/download-info', async (req, res) => {
  try {
    const { accountId, bucketName, objectKey } = req.params;

    const account = storage.getAccountById(accountId);
    if (!account) {
      return res.status(404).json({ error: '账号不存在' });
    }

    const auth = account.email ? { email: account.email, key: account.apiToken } : account.apiToken;
    const cfAccountId = await cfApi.getAccountId(auth);

    // 获取存储桶详情，其中包含公开访问 URL
    let publicUrl = null;
    try {
      const bucketInfo = await cfApi.getR2Bucket(auth, cfAccountId, bucketName);
      // R2 公开访问 URL 在 bucket 配置中
      if (bucketInfo && bucketInfo.public_url_base) {
        publicUrl = `${bucketInfo.public_url_base}/${objectKey}`;
      }
    } catch (e) {
      logger.warn('获取 R2 存储桶详情失败:', e.message);
    }

    res.json({
      success: true,
      publicUrl: publicUrl,
      objectKey: objectKey,
      bucketName: bucketName
    });
  } catch (e) {
    logger.error('获取 R2 下载信息失败:', e.message);
    res.status(500).json({ error: e.message });
  }
});


module.exports = router;
