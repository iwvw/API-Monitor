const express = require('express');
const router = express.Router();
const storage = require('./storage');
const axios = require('axios');

// 辅助函数：获取账号并创建 axios 实例
const getClient = (accountId) => {
    const account = storage.getAccountById(accountId);
    if (!account) throw new Error('Account not found');

    // 处理 Base URL，去掉末尾斜杠
    const baseURL = account.api_url.endsWith('/') ? account.api_url.slice(0, -1) : account.api_url;

    const client = axios.create({
        baseURL: baseURL,
        headers: {
            'Authorization': account.api_token,
            'Content-Type': 'application/json'
        },
        timeout: 15000
    });

    return { client, account }; // 返回 account 以便需要时使用
};

// 获取所有账号
router.get('/manage-accounts', (req, res) => {
    try {
        const accounts = storage.getAllAccounts();
        // 隐藏 token
        const safeAccounts = accounts.map(acc => ({
            ...acc,
            api_token: acc.api_token ? '******' : ''
        }));
        res.json({ success: true, data: safeAccounts });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

// 添加账号
router.post('/manage-accounts', (req, res) => {
    try {
        const id = storage.addAccount(req.body);
        res.json({ success: true, data: { id } });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

// 删除账号
router.delete('/manage-accounts/:id', (req, res) => {
    try {
        storage.deleteAccount(req.params.id);
        res.json({ success: true });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

// 账号连通性测试 (探活)
router.post('/manage-accounts/:id/test', async (req, res) => {
    try {
        const { client, account } = getClient(req.params.id);

        // 使用 /api/me 验证 Token 有效性
        const response = await client.get('/api/me');

        const status = response.status === 200 ? 'online' : 'error';
        // 尝试从响应头或数据中获取版本，如果没有则为 unknown
        const version = response.headers['server'] || 'unknown';

        storage.updateStatus(account.id, status, version);
        res.json({ success: true, data: { status, version, user: response.data.data } });
    } catch (error) {
        // 如果是 401，说明 token 无效但在线
        const status = error.response?.status === 401 ? 'auth_failed' : 'offline';
        storage.updateStatus(req.params.id, status, null);
        res.json({ success: true, data: { status, error: error.message } });
    }
});

// --- OpenList 功能接口 ---

// 1. 获取当前用户信息
router.get('/:id/me', async (req, res) => {
    try {
        const { client } = getClient(req.params.id);
        const response = await client.get('/api/me');
        res.json(response.data);
    } catch (error) {
        res.status(error.response?.status || 500).json(error.response?.data || { success: false, error: error.message });
    }
});

// 2. 列出文件/目录
router.post('/:id/fs/list', async (req, res) => {
    try {
        const { client, account } = getClient(req.params.id);
        // 构造请求体，设置默认值
        const payload = {
            path: req.body.path || '/',
            password: req.body.password || '',
            page: req.body.page || 1,
            per_page: req.body.per_page || 0,
            refresh: req.body.refresh || false
        };
        const response = await client.post('/api/fs/list', payload);

        // API 调用成功，自动更新状态为 online
        if (response.data.code === 200 && account.status !== 'online') {
            storage.updateStatus(account.id, 'online', account.version);
        }

        res.json(response.data);
    } catch (error) {
        // API 调用失败，更新状态为 offline
        const account = storage.getAccount(req.params.id);
        if (account) {
            storage.updateStatus(account.id, 'offline', account.version);
        }
        res.status(error.response?.status || 500).json(error.response?.data || { success: false, error: error.message });
    }
});

// 3. 获取文件/目录详情
router.post('/:id/fs/get', async (req, res) => {
    try {
        const { client } = getClient(req.params.id);
        const payload = {
            path: req.body.path,
            password: req.body.password || ''
        };
        const response = await client.post('/api/fs/get', payload);
        res.json(response.data);
    } catch (error) {
        res.status(error.response?.status || 500).json(error.response?.data || { success: false, error: error.message });
    }
});

// 4. 管理员: 列出存储
router.get('/:id/admin/storages', async (req, res) => {
    try {
        const { client } = getClient(req.params.id);
        const response = await client.get('/api/admin/storage/list');
        res.json(response.data);
    } catch (error) {
        res.status(error.response?.status || 500).json(error.response?.data || { success: false, error: error.message });
    }
});

// 5. 搜索文件
router.post('/:id/fs/search', async (req, res) => {
    try {
        const { client } = getClient(req.params.id);
        const payload = {
            parent: req.body.parent || '/',
            keywords: req.body.keywords,
            page: req.body.page || 1,
            per_page: req.body.per_page || 100,
            scope: req.body.scope || 0 // 0: all, 1: folder, 2: file
        };
        const response = await client.post('/api/fs/search', payload);
        res.json(response.data);
    } catch (error) {
        res.status(error.response?.status || 500).json(error.response?.data || { success: false, error: error.message });
    }
});

// --- 模块通用代理接口 ---
router.all('/:id/proxy/*', async (req, res) => {
    const { id } = req.params;
    const subPath = req.params[0];

    try {
        const { client } = getClient(id);
        const response = await client({
            method: req.method,
            url: `/api/${subPath}`,
            data: req.body,
            params: req.query
        });
        res.json(response.data);
    } catch (error) {
        res.status(error.response?.status || 500).json(error.response?.data || { success: false, error: error.message });
    }
});

// --- 模块设置接口 ---
router.get('/settings/:key', (req, res) => {
    try {
        const value = storage.getSetting(req.params.key);
        res.json({ success: true, value });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

router.post('/settings', (req, res) => {
    try {
        const { key, value } = req.body;
        storage.setSetting(key, value);
        res.json({ success: true });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

module.exports = router;
