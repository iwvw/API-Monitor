const express = require('express');
const router = express.Router();
const tencentApi = require('./tencent-api');
const { createLogger } = require('../../src/utils/logger');
const db = require('../../src/db/database');

const logger = createLogger('TencentAPI');

// 中间件：获取并验证腾讯云账号
async function getAccount(req, res, next) {
    const accountId = req.params.accountId || req.query.accountId;
    if (!accountId) {
        return res.status(400).json({ error: 'Missing accountId' });
    }

    try {
        const database = db.getDatabase();
        const account = database.prepare('SELECT * FROM tencent_accounts WHERE id = ?').get(accountId);

        if (!account) {
            return res.status(404).json({ error: 'Account not found' });
        }

        req.tencentAuth = {
            secretId: account.secret_id,
            secretKey: account.secret_key,
            regionId: account.region_id
        };
        next();
    } catch (error) {
        logger.error('获取账号失败:', error);
        res.status(500).json({ error: 'Database error' });
    }
}

// ==================== 账号管理 ====================

// 获取所有账号
router.get('/accounts', (req, res) => {
    try {
        const database = db.getDatabase();
        const accounts = database.prepare('SELECT id, name, secret_id, region_id, description, is_default, created_at FROM tencent_accounts ORDER BY created_at DESC').all();

        // 脱敏处理
        const normalizedAccounts = accounts.map(acc => ({
            ...acc,
            secret_id: acc.secret_id ? acc.secret_id.slice(0, 8) + '****' + acc.secret_id.slice(-4) : '-'
        }));

        res.json(normalizedAccounts);
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

// 添加账号
router.post('/accounts', (req, res) => {
    const { name, secretId, secretKey, regionId, description } = req.body;
    if (!name || !secretId || !secretKey) {
        return res.status(400).json({ error: 'Missing required fields' });
    }

    try {
        const database = db.getDatabase();
        const result = database.prepare(`
            INSERT INTO tencent_accounts (name, secret_id, secret_key, region_id, description)
            VALUES (?, ?, ?, ?, ?)
        `).run(name, secretId, secretKey, regionId || 'ap-guangzhou', description || '');

        res.json({ id: result.lastInsertRowid, success: true });
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

// 更新账号
router.put('/accounts/:id', (req, res) => {
    const { name, secretId, secretKey, regionId, description } = req.body;
    try {
        const database = db.getDatabase();
        if (secretKey) {
            database.prepare(`
                UPDATE tencent_accounts 
                SET name = ?, secret_id = ?, secret_key = ?, region_id = ?, description = ?, updated_at = CURRENT_TIMESTAMP
                WHERE id = ?
            `).run(name, secretId, secretKey, regionId, description || '', req.params.id);
        } else {
            database.prepare(`
                UPDATE tencent_accounts 
                SET name = ?, secret_id = ?, region_id = ?, description = ?, updated_at = CURRENT_TIMESTAMP
                WHERE id = ?
            `).run(name, secretId, regionId, description || '', req.params.id);
        }
        res.json({ success: true });
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

// 删除账号
router.delete('/accounts/:id', (req, res) => {
    try {
        const database = db.getDatabase();
        database.prepare('DELETE FROM tencent_accounts WHERE id = ?').run(req.params.id);
        res.json({ success: true });
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

// ==================== DNS 管理 ====================

// 获取域名列表
router.get('/accounts/:accountId/domains', getAccount, async (req, res) => {
    try {
        logger.info(`Fetching domains for account ${req.params.accountId}`);
        const result = await tencentApi.listDomains(req.tencentAuth);
        res.json(result);
    } catch (error) {
        logger.error('listDomains failed:', error.message, error.stack);
        res.status(500).json({ error: error.message });
    }
});

// 添加域名
router.post('/accounts/:accountId/domains', getAccount, async (req, res) => {
    const { domain } = req.body;
    try {
        const result = await tencentApi.addDomain(req.tencentAuth, domain);
        res.json({ success: true, result });
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

// 删除域名
router.delete('/accounts/:accountId/domains/:domain', getAccount, async (req, res) => {
    try {
        const result = await tencentApi.deleteDomain(req.tencentAuth, req.params.domain);
        res.json({ success: true, result });
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

// 获取记录列表
router.get('/accounts/:accountId/domains/:domain/records', getAccount, async (req, res) => {
    try {
        const result = await tencentApi.listDomainRecords(req.tencentAuth, req.params.domain);
        res.json(result);
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

// 添加记录
router.post('/accounts/:accountId/domains/:domain/records', getAccount, async (req, res) => {
    try {
        const result = await tencentApi.addDomainRecord(req.tencentAuth, req.params.domain, req.body);
        res.json({ success: true, result });
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

// 更新记录
router.put('/accounts/:accountId/domains/:domain/records/:recordId', getAccount, async (req, res) => {
    try {
        const result = await tencentApi.updateDomainRecord(req.tencentAuth, req.params.domain, req.params.recordId, req.body);
        res.json({ success: true, result });
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

// 删除记录
router.delete('/accounts/:accountId/domains/:domain/records/:recordId', getAccount, async (req, res) => {
    try {
        const result = await tencentApi.deleteDomainRecord(req.tencentAuth, req.params.domain, req.params.recordId);
        res.json({ success: true, result });
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

// 修改记录状态
router.patch('/accounts/:accountId/domains/:domain/records/:recordId/status', getAccount, async (req, res) => {
    const { status } = req.body;
    try {
        const result = await tencentApi.setDomainRecordStatus(req.tencentAuth, req.params.domain, req.params.recordId, status);
        res.json({ success: true, result });
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

// ==================== CVM 管理 ====================

// 获取所有 CVM
router.get('/accounts/:accountId/cvm', getAccount, async (req, res) => {
    try {
        const result = await tencentApi.listAllCvmInstances(req.tencentAuth);
        res.json(result);
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

// 控制 CVM
router.post('/accounts/:accountId/cvm/:instanceId/control', getAccount, async (req, res) => {
    const { action, region } = req.body;
    try {
        const result = await tencentApi.controlCvmInstance(req.tencentAuth, region, req.params.instanceId, action);
        res.json({ success: true, result });
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

// ==================== Lighthouse 管理 ====================

// 获取所有轻量服务器
router.get('/accounts/:accountId/lighthouse', getAccount, async (req, res) => {
    try {
        const result = await tencentApi.listAllLighthouseInstances(req.tencentAuth);
        res.json(result);
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

// 控制轻量服务器
router.post('/accounts/:accountId/lighthouse/:instanceId/control', getAccount, async (req, res) => {
    const { action, region } = req.body;
    try {
        const result = await tencentApi.controlLighthouseInstance(req.tencentAuth, region, req.params.instanceId, action);
        res.json({ success: true, result });
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

module.exports = router;
