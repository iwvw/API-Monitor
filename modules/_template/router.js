/**
 * 模块 API 路由
 */

const express = require('express');
const router = express.Router();
const storage = require('./storage');
// const apiService = require('./service'); // 引入你的 API 服务
const { createLogger } = require('../../src/utils/logger');

const logger = createLogger('{{ModuleName}}');

/**
 * 获取所有账号
 */
router.get('/accounts', async (req, res) => {
  try {
    const serverAccounts = storage.loadAccounts();
    const envAccounts = storage.getEnvAccounts();
    res.json([...envAccounts, ...serverAccounts]);
  } catch (error) {
    logger.error('获取账号失败', error.message);
    res.status(500).json({ error: error.message });
  }
});

/**
 * 验证并添加账号
 */
router.post('/validate-account', async (req, res) => {
  const { name, token } = req.body;
  if (!name || !token) {
    return res.status(400).json({ error: '参数缺失' });
  }

  try {
    // 调用 apiService 验证 token
    // const userData = await apiService.validateToken(token);

    res.json({
      success: true,
      message: '验证成功',
      // data: userData
    });
  } catch (error) {
    res.status(400).json({ error: '验证失败: ' + error.message });
  }
});

/**
 * 保存账号列表 (主机存储)
 */
router.post('/server-accounts', async (req, res) => {
  const { accounts } = req.body;
  if (!Array.isArray(accounts)) {
    return res.status(400).json({ error: '无效的数据格式' });
  }

  if (storage.saveAccounts(accounts)) {
    res.json({ success: true });
  } else {
    res.status(500).json({ error: '保存失败' });
  }
});

module.exports = router;
