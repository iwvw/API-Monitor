/**
 * Koyeb API 路由模块
 */

const express = require('express');
const router = express.Router();
const koyebApi = require('./koyeb-api');
const { KoyebAccount } = require('./models');

const { requireAuth } = require('../../src/middleware/auth');

// ============ 账号管理 API ============

/**
 * 获取所有 Koyeb 账号列表 (导出用，包含 Token)
 */
router.get('/koyeb/accounts/export', requireAuth, (req, res) => {
    try {
        const accounts = KoyebAccount.findAll();
        res.json({ success: true, accounts });
    } catch (error) {
        console.error('导出 Koyeb 账号列表失败:', error);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 获取所有 Koyeb 账号列表 (展示用，隐藏 Token)
 */
router.get('/koyeb/accounts', requireAuth, (req, res) => {
    try {
        const accounts = KoyebAccount.findAll();
        // 隐藏敏感信息
        const safeAccounts = accounts.map(acc => ({
            id: acc.id,
            name: acc.name,
            email: acc.email || '',
            status: acc.status || 'unknown',
            balance: acc.balance || null,
            createdAt: acc.created_at
        }));
        res.json({ success: true, accounts: safeAccounts });
    } catch (error) {
        console.error('获取 Koyeb 账号列表失败:', error);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 添加 Koyeb 账号
 */
router.post('/koyeb/accounts', async (req, res) => {
    try {
        const { name, token } = req.body;

        if (!name || !token) {
            return res.status(400).json({ success: false, error: '名称和 Token 不能为空' });
        }

        // 验证 Token
        const accountData = await koyebApi.fetchAccountData(token);

        // 保存到数据库
        const account = KoyebAccount.createAccount({
            name,
            token,
            email: accountData.user?.email || '',
            balance: accountData.balance || 0,
            status: 'active'
        });

        res.json({
            success: true,
            account: {
                id: account.id,
                name: account.name,
                email: account.email,
                status: account.status
            },
            data: accountData
        });
    } catch (error) {
        console.error('添加 Koyeb 账号失败:', error);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 删除 Koyeb 账号
 */
router.delete('/koyeb/accounts/:id', (req, res) => {
    try {
        const { id } = req.params;
        KoyebAccount.deleteAccount(id);
        res.json({ success: true });
    } catch (error) {
        console.error('删除 Koyeb 账号失败:', error);
        res.status(500).json({ success: false, error: error.message });
    }
});

// ============ 数据获取 API ============

/**
 * 获取所有账号的完整数据（用于监控页面）
 */
router.get('/koyeb/data', async (req, res) => {
    try {
        const accounts = KoyebAccount.findAll();

        const results = await Promise.all(accounts.map(async (acc) => {
            try {
                const data = await koyebApi.fetchAccountData(acc.token);

                // 更新账号余额和状态
                KoyebAccount.updateAccount(acc.id, {
                    balance: data.balance || 0,
                    email: data.user?.email || acc.email,
                    status: 'active'
                });

                return {
                    id: acc.id,  // 添加账号 ID
                    name: acc.name,
                    data: data.user,
                    projects: data.projects,
                    balance: data.balance,
                    organization: data.organization,
                    error: null
                };
            } catch (error) {
                console.error(`获取账号 ${acc.name} 数据失败:`, error);

                KoyebAccount.updateAccount(acc.id, { status: 'error' });

                return {
                    id: acc.id,  // 添加账号 ID
                    name: acc.name,
                    data: null,
                    projects: [],
                    balance: null,
                    error: error.message
                };
            }
        }));

        res.json({ success: true, accounts: results });
    } catch (error) {
        console.error('获取 Koyeb 数据失败:', error);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 刷新单个账号数据
 */
router.post('/koyeb/accounts/:id/refresh', async (req, res) => {
    try {
        const { id } = req.params;
        const account = KoyebAccount.findById(id);

        if (!account) {
            return res.status(404).json({ success: false, error: '账号不存在' });
        }

        const data = await koyebApi.fetchAccountData(account.token);

        KoyebAccount.updateAccount(id, {
            balance: data.balance || 0,
            email: data.user?.email || account.email,
            status: 'active'
        });

        res.json({ success: true, data });
    } catch (error) {
        console.error('刷新账号数据失败:', error);
        res.status(500).json({ success: false, error: error.message });
    }
});

// ============ 服务管理 API ============

/**
 * 暂停服务
 */
router.post('/koyeb/services/:serviceId/pause', async (req, res) => {
    try {
        const { serviceId } = req.params;
        const { accountId } = req.body;

        const account = KoyebAccount.findById(accountId);
        if (!account) {
            return res.status(404).json({ success: false, error: '账号不存在' });
        }

        await koyebApi.pauseService(account.token, serviceId);
        res.json({ success: true });
    } catch (error) {
        console.error('暂停服务失败:', error);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 重启/恢复服务
 */
router.post('/koyeb/services/:serviceId/restart', async (req, res) => {
    try {
        const { serviceId } = req.params;
        const { accountId } = req.body;

        const account = KoyebAccount.findById(accountId);
        if (!account) {
            return res.status(404).json({ success: false, error: '账号不存在' });
        }

        await koyebApi.restartService(account.token, serviceId);
        res.json({ success: true });
    } catch (error) {
        console.error('重启服务失败:', error);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 重新部署服务
 */
router.post('/koyeb/services/:serviceId/redeploy', async (req, res) => {
    try {
        const { serviceId } = req.params;
        const { accountId } = req.body;

        const account = KoyebAccount.findById(accountId);
        if (!account) {
            return res.status(404).json({ success: false, error: '账号不存在' });
        }

        await koyebApi.redeployService(account.token, serviceId);
        res.json({ success: true });
    } catch (error) {
        console.error('重新部署服务失败:', error);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 删除服务
 */
router.delete('/koyeb/services/:serviceId', async (req, res) => {
    try {
        const { serviceId } = req.params;
        const { accountId } = req.body;

        const account = KoyebAccount.findById(accountId);
        if (!account) {
            return res.status(404).json({ success: false, error: '账号不存在' });
        }

        await koyebApi.deleteService(account.token, serviceId);
        res.json({ success: true });
    } catch (error) {
        console.error('删除服务失败:', error);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 删除应用
 */
router.delete('/koyeb/apps/:appId', async (req, res) => {
    try {
        const { appId } = req.params;
        const { accountId } = req.body;

        const account = KoyebAccount.findById(accountId);
        if (!account) {
            return res.status(404).json({ success: false, error: '账号不存在' });
        }

        await koyebApi.deleteApp(account.token, appId);
        res.json({ success: true });
    } catch (error) {
        console.error('删除应用失败:', error);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 重命名应用
 */
router.post('/koyeb/apps/:appId/rename', async (req, res) => {
    try {
        const { appId } = req.params;
        const { accountId, name } = req.body;

        const account = KoyebAccount.findById(accountId);
        if (!account) {
            return res.status(404).json({ success: false, error: '账号不存在' });
        }

        await koyebApi.renameApp(account.token, appId, name);
        res.json({ success: true });
    } catch (error) {
        console.error('重命名应用失败:', error);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 重命名服务
 */
router.post('/koyeb/services/:serviceId/rename', async (req, res) => {
    try {
        const { serviceId } = req.params;
        const { accountId, name } = req.body;

        const account = KoyebAccount.findById(accountId);
        if (!account) {
            return res.status(404).json({ success: false, error: '账号不存在' });
        }

        await koyebApi.renameService(account.token, serviceId, name);
        res.json({ success: true });
    } catch (error) {
        console.error('重命名服务失败:', error);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 获取服务日志
 */
router.get('/koyeb/services/:serviceId/logs', async (req, res) => {
    try {
        const { serviceId } = req.params;
        const { accountId, limit = 100 } = req.query;

        const account = KoyebAccount.findById(accountId);
        if (!account) {
            return res.status(404).json({ success: false, error: '账号不存在' });
        }

        const logs = await koyebApi.fetchServiceLogs(account.token, serviceId, limit);
        res.json({ success: true, logs });
    } catch (error) {
        console.error('获取服务日志失败:', error);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 获取服务实例
 */
router.get('/koyeb/services/:serviceId/instances', async (req, res) => {
    try {
        const { serviceId } = req.params;
        const { accountId } = req.query;

        const account = KoyebAccount.findById(accountId);
        if (!account) {
            return res.status(404).json({ success: false, error: '账号不存在' });
        }

        const instances = await koyebApi.fetchServiceInstances(account.token, serviceId);
        res.json({ success: true, instances });
    } catch (error) {
        console.error('获取服务实例失败:', error);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 获取服务指标
 */
router.get('/koyeb/services/:serviceId/metrics', async (req, res) => {
    try {
        const { serviceId } = req.params;
        const { accountId, instanceId, name, start, end } = req.query;

        const account = KoyebAccount.findById(accountId);
        if (!account) {
            return res.status(404).json({ success: false, error: '账号不存在' });
        }

        const metrics = await koyebApi.fetchServiceMetrics(account.token, serviceId, instanceId, name, start, end);
        res.json({ success: true, metrics });
    } catch (error) {
        console.error('获取服务指标失败:', error);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 获取组织用量
 */
router.get('/koyeb/usage', async (req, res) => {
    try {
        const { accountId, start, end } = req.query;

        const account = KoyebAccount.findById(accountId);
        if (!account) {
            return res.status(404).json({ success: false, error: '账号不存在' });
        }

        const usage = await koyebApi.fetchOrganizationUsage(account.token, start, end);
        res.json({ success: true, usage });
    } catch (error) {
        console.error('获取组织用量失败:', error);
        res.status(500).json({ success: false, error: error.message });
    }
});

module.exports = router;
