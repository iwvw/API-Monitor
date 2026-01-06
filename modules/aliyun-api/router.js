const express = require('express');
const router = express.Router();
const aliyunApi = require('./aliyun-api');
const { createLogger } = require('../../src/utils/logger');
const db = require('../../src/db/database');

const logger = createLogger('AliyunAPI');

// 调试日志中间件
router.use((req, res, next) => {
    logger.info(`[Router Request] ${req.method} ${req.path}`);
    next();
});

// 中间件：获取并验证阿里云账号
async function getAccount(req, res, next) {
    const accountId = req.params.accountId || req.query.accountId;
    logger.info(`[getAccount] accountId: ${accountId}`);
    if (!accountId) {
        return res.status(400).json({ error: 'Missing accountId' });
    }

    try {
        const database = db.getDatabase();
        const account = database.prepare('SELECT * FROM aliyun_accounts WHERE id = ?').get(accountId);

        if (!account) {
            return res.status(404).json({ error: 'Account not found' });
        }

        req.aliyunAuth = {
            accessKeyId: account.access_key_id,
            accessKeySecret: account.access_key_secret,
            regionId: account.region_id
        };
        next();
    } catch (error) {
        logger.error('获取账号失败:', error);
        res.status(500).json({ error: 'Database error' });
    }
}

// ==================== 账号管理 ====================

// 获取监控数据
router.post('/accounts/:accountId/metrics', getAccount, async (req, res) => {
    try {
        const result = await aliyunApi.getMetricData(req.aliyunAuth, req.body);
        res.json(result);
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

// 获取防火墙规则
router.get('/accounts/:accountId/swas/:instanceId/firewall', getAccount, async (req, res) => {
    const { regionId } = req.query;
    try {
        const result = await aliyunApi.listFirewallRules(req.aliyunAuth, regionId, req.params.instanceId);
        res.json(result);
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

// 添加防火墙规则
router.post('/accounts/:accountId/swas/:instanceId/firewall', getAccount, async (req, res) => {
    const { rule } = req.body;
    const { regionId } = req.body;
    try {
        const result = await aliyunApi.createFirewallRule(req.aliyunAuth, regionId, req.params.instanceId, rule);
        res.json({ success: true, result });
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

// 删除防火墙规则
router.delete('/accounts/:accountId/swas/:instanceId/firewall/:ruleId', getAccount, async (req, res) => {
    const { regionId } = req.query;
    try {
        const result = await aliyunApi.deleteFirewallRule(req.aliyunAuth, regionId, req.params.instanceId, req.params.ruleId);
        res.json({ success: true, result });
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

// 获取所有账号 (返回脱敏的 AccessKey ID)
router.get('/accounts', (req, res) => {
    try {
        const database = db.getDatabase();
        const accounts = database.prepare('SELECT id, name, access_key_id, region_id, description, is_default, created_at FROM aliyun_accounts ORDER BY created_at DESC').all();

        // 脱敏处理并确保字段名兼容
        const normalizedAccounts = accounts.map(acc => {
            const maskedId = acc.access_key_id ?
                acc.access_key_id.slice(0, 8) + '****' + acc.access_key_id.slice(-4) : '-';

            return {
                ...acc,
                // 同时提供下划线和驼峰命名，确保模板和逻辑都能访问到
                accessKeyId: maskedId,
                access_key_id: maskedId,
                regionId: acc.region_id,
                region_id: acc.region_id
            };
        });

        res.json(normalizedAccounts);
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

// 添加账号
router.post('/accounts', (req, res) => {
    const { name, accessKeyId, accessKeySecret, regionId, description } = req.body;
    if (!name || !accessKeyId || !accessKeySecret) {
        return res.status(400).json({ error: 'Missing required fields' });
    }

    try {
        const database = db.getDatabase();
        const result = database.prepare(`
            INSERT INTO aliyun_accounts (name, access_key_id, access_key_secret, region_id, description)
            VALUES (?, ?, ?, ?, ?)
        `).run(name, accessKeyId, accessKeySecret, regionId || 'cn-hangzhou', description || '');

        res.json({ success: true, id: result.lastInsertRowid });
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

// 更新账号
router.put('/accounts/:id', (req, res) => {
    const { name, accessKeyId, accessKeySecret, regionId, description } = req.body;

    try {
        const database = db.getDatabase();

        // 如果提供了新的 AccessKey，则更新；否则只更新其他字段
        if (accessKeyId && accessKeySecret) {
            database.prepare(`
                UPDATE aliyun_accounts 
                SET name = ?, access_key_id = ?, access_key_secret = ?, region_id = ?, description = ?, updated_at = CURRENT_TIMESTAMP
                WHERE id = ?
            `).run(name, accessKeyId, accessKeySecret, regionId || 'cn-hangzhou', description || '', req.params.id);
        } else {
            database.prepare(`
                UPDATE aliyun_accounts 
                SET name = ?, region_id = ?, description = ?, updated_at = CURRENT_TIMESTAMP
                WHERE id = ?
            `).run(name, regionId || 'cn-hangzhou', description || '', req.params.id);
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
        database.prepare('DELETE FROM aliyun_accounts WHERE id = ?').run(req.params.id);
        res.json({ success: true });
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

// ==================== DNS 管理 ====================

// 获取域名列表
router.get('/accounts/:accountId/domains', getAccount, async (req, res) => {
    try {
        const result = await aliyunApi.listDomains(req.aliyunAuth, {
            PageSize: req.query.pageSize,
            PageNumber: req.query.pageNumber,
            KeyWord: req.query.keyword
        });
        res.json(result);
    } catch (error) {
        logger.error('获取域名列表失败:', error.message);
        res.status(500).json({ error: error.message });
    }
});

// 添加域名
router.post('/accounts/:accountId/domains', getAccount, async (req, res) => {
    const { domainName } = req.body;
    if (!domainName) return res.status(400).json({ error: 'Missing domainName' });

    try {
        const result = await aliyunApi.addDomain(req.aliyunAuth, domainName);
        res.json({ success: true, result });
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

// 删除域名
router.delete('/accounts/:accountId/domains/:domainName', getAccount, async (req, res) => {
    try {
        const result = await aliyunApi.deleteDomain(req.aliyunAuth, req.params.domainName);
        res.json({ success: true, result });
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

// 获取解析记录
router.get('/accounts/:accountId/domains/:domainName/records', getAccount, async (req, res) => {
    try {
        const result = await aliyunApi.listDomainRecords(req.aliyunAuth, req.params.domainName, {
            PageSize: req.query.pageSize,
            PageNumber: req.query.pageNumber,
            RRKeyWord: req.query.rrKeyword,
            TypeKeyWord: req.query.typeKeyword,
            ValueKeyWord: req.query.valueKeyword
        });
        res.json(result);
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

// 添加解析记录
router.post('/accounts/:accountId/domains/:domainName/records', getAccount, async (req, res) => {
    try {
        const params = {
            DomainName: req.params.domainName,
            RR: req.body.rr,
            Type: req.body.type,
            Value: req.body.value,
            TTL: req.body.ttl,
            Priority: req.body.priority,
            Line: req.body.line
        };
        const result = await aliyunApi.addDomainRecord(req.aliyunAuth, params);
        res.json({ success: true, result });
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

// 修改解析记录
router.put('/accounts/:accountId/records/:recordId', getAccount, async (req, res) => {
    try {
        const params = {
            RecordId: req.params.recordId,
            RR: req.body.rr,
            Type: req.body.type,
            Value: req.body.value,
            TTL: req.body.ttl,
            Priority: req.body.priority,
            Line: req.body.line
        };
        const result = await aliyunApi.updateDomainRecord(req.aliyunAuth, params);
        res.json({ success: true, result });
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

// 删除解析记录
router.delete('/accounts/:accountId/records/:recordId', getAccount, async (req, res) => {
    try {
        const result = await aliyunApi.deleteDomainRecord(req.aliyunAuth, req.params.recordId);
        res.json({ success: true, result });
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

// 设置解析记录状态
router.put('/accounts/:accountId/records/:recordId/status', getAccount, async (req, res) => {
    try {
        const result = await aliyunApi.setDomainRecordStatus(req.aliyunAuth, req.params.recordId, req.body.status);
        res.json({ success: true, result });
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

// ==================== ECS 管理 ====================

// 获取实例列表 (自动查询所有区域)
router.get('/accounts/:accountId/instances', getAccount, async (req, res) => {
    try {
        const result = await aliyunApi.listInstances(req.aliyunAuth, {
            PageSize: req.query.pageSize,
            PageNumber: req.query.pageNumber,
            allRegions: true // 自动查询所有区域
        });

        // 增强数据
        if (result.instances) {
            result.instances = result.instances.map(inst => ({
                ...inst,
                RegionName: aliyunApi.REGION_MAP[inst.RegionId] || inst.RegionId,
                InstanceTypeFriendly: aliyunApi.formatFlavor(inst.InstanceType)
            }));
        }

        res.json(result);
    } catch (error) {
        logger.error('获取ECS实例失败:', error.message);
        res.status(500).json({ error: error.message });
    }
});

// 启动实例
router.post('/accounts/:accountId/instances/:instanceId/start', getAccount, async (req, res) => {
    const { regionId } = req.body;
    try {
        const result = await aliyunApi.startInstance(req.aliyunAuth, regionId, req.params.instanceId);
        res.json({ success: true, result });
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

// 停止实例
router.post('/accounts/:accountId/instances/:instanceId/stop', getAccount, async (req, res) => {
    const { regionId, force } = req.body;
    try {
        const result = await aliyunApi.stopInstance(req.aliyunAuth, regionId, req.params.instanceId, force);
        res.json({ success: true, result });
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

// 重启实例
router.post('/accounts/:accountId/instances/:instanceId/reboot', getAccount, async (req, res) => {
    const { regionId, force } = req.body;
    try {
        const result = await aliyunApi.rebootInstance(req.aliyunAuth, regionId, req.params.instanceId, force);
        res.json({ success: true, result });
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

// ==================== 轻量应用服务器 (SWAS) 管理 ====================

// 获取轻量服务器列表 (自动查询所有区域)
router.get('/accounts/:accountId/swas', getAccount, async (req, res) => {
    try {
        const result = await aliyunApi.listSwasInstances(req.aliyunAuth, {
            pageSize: req.query.pageSize,
            pageNumber: req.query.pageNumber
        });

        // 增强数据
        if (result.instances) {
            result.instances = result.instances.map(inst => ({
                ...inst,
                RegionName: aliyunApi.REGION_MAP[inst.RegionId] || inst.RegionId,
                InstanceTypeFriendly: aliyunApi.formatFlavor(inst.PlanId) // 轻量用 PlanId 作为规格
            }));
        }

        res.json(result);
    } catch (error) {
        logger.error('获取SWAS实例失败:', error.message);
        res.status(500).json({ error: error.message });
    }
});

// 启动轻量服务器
router.post('/accounts/:accountId/swas/:instanceId/start', getAccount, async (req, res) => {
    const { regionId } = req.body;
    if (!regionId) return res.status(400).json({ error: 'Missing regionId' });
    try {
        const result = await aliyunApi.startSwasInstance(req.aliyunAuth, regionId, req.params.instanceId);
        res.json({ success: true, result });
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

// 停止轻量服务器
router.post('/accounts/:accountId/swas/:instanceId/stop', getAccount, async (req, res) => {
    const { regionId, force } = req.body;
    if (!regionId) return res.status(400).json({ error: 'Missing regionId' });
    try {
        const result = await aliyunApi.stopSwasInstance(req.aliyunAuth, regionId, req.params.instanceId, force);
        res.json({ success: true, result });
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

// 重启轻量服务器
router.post('/accounts/:accountId/swas/:instanceId/reboot', getAccount, async (req, res) => {
    const { regionId, force } = req.body;
    if (!regionId) return res.status(400).json({ error: 'Missing regionId' });
    try {
        const result = await aliyunApi.rebootSwasInstance(req.aliyunAuth, regionId, req.params.instanceId, force);
        res.json({ success: true, result });
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

// 兜底 404
router.use((req, res) => {
    logger.warn(`[Router 404 Fallback] ${req.method} ${req.originalUrl} -> No match in Aliyun Router`);
    res.status(404).json({ error: `Path not found in Aliyun router: ${req.path}` });
});

module.exports = router;
