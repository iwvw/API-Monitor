/**
 * Zeabur API 管理 - API 路由
 */

const express = require('express');
const router = express.Router();
const storage = require('./storage');
const zeaburApi = require('./zeabur-api');

/**
 * 临时账号API - 获取账号信息
 */
router.post('/temp-accounts', async (req, res) => {
  try {
    const { accounts } = req.body;

    console.log('📥 收到账号请求:', accounts?.length, '个账号');

    if (!accounts || !Array.isArray(accounts)) {
      return res.status(400).json({ error: '无效的账号列表' });
    }

    const results = await Promise.all(accounts.map(async (account) => {
      try {
        console.log(`🔍 正在获取账号 [${account.name}] 的数据...`);
        const { user, projects, aihub, serviceCosts } = await zeaburApi.fetchAccountData(account.token);
        console.log(`   API 返回的 credit: ${user.credit}, serviceCosts: $${serviceCosts}`);

        let usageData = { totalUsage: 0, freeQuotaRemaining: 5, freeQuotaLimit: 5 };
        if (user._id) {
          try {
            usageData = await zeaburApi.fetchUsageData(account.token, user._id, projects);
            console.log(`💰 [${account.name}] 用量: $${usageData.totalUsage.toFixed(2)}, 剩余: $${usageData.freeQuotaRemaining.toFixed(2)}`);
          } catch (e) {
            console.log(`⚠️ [${account.name}] 获取用量失败:`, e.message);
          }
        }

        const creditInCents = Math.round(usageData.freeQuotaRemaining * 100);

        return {
          name: account.name,
          success: true,
          data: {
            ...user,
            credit: creditInCents,
            totalUsage: usageData.totalUsage,
            totalCost: usageData.totalUsage,
            freeQuotaLimit: usageData.freeQuotaLimit
          },
          aihub: aihub
        };
      } catch (error) {
        console.error(`❌ [${account.name}] 错误:`, error.message);
        return {
          name: account.name,
          success: false,
          error: error.message
        };
      }
    }));

    console.log('📤 返回结果:', results.length, '个账号');
    res.json(results);
  } catch (error) {
    console.error('❌ /api/temp-accounts 未捕获异常:', error);
    res.status(500).json({ error: '/api/temp-accounts 服务器错误: ' + error.message });
  }
});

/**
 * 临时账号API - 获取项目信息
 */
router.post('/temp-projects', async (req, res) => {
  try {
    const { accounts } = req.body;

    console.log('📥 收到项目请求:', accounts?.length, '个账号');

    if (!accounts || !Array.isArray(accounts)) {
      return res.status(400).json({ error: '无效的账号列表' });
    }

    const results = await Promise.all(accounts.map(async (account) => {
      try {
        console.log(`🔍 正在获取账号 [${account.name}] 的项目...`);
        const { user, projects } = await zeaburApi.fetchAccountData(account.token);

        let projectCosts = {};
        if (user._id) {
          try {
            const usageData = await zeaburApi.fetchUsageData(account.token, user._id, projects);
            projectCosts = usageData.projectCosts;
          } catch (e) {
            console.log(`⚠️ [${account.name}] 获取用量失败:`, e.message);
          }
        }

        console.log(`📦 [${account.name}] 找到 ${projects.length} 个项目`);

        const projectsWithCost = projects.map(project => {
          const pid = project && (project._id || project.id || (project._id && project._id.$oid)) || '';
          let rawCost = 0;
          if (pid && projectCosts[pid] !== undefined) rawCost = projectCosts[pid];
          else if (project && projectCosts[project.id] !== undefined) rawCost = projectCosts[project.id];
          else rawCost = 0;

          const cost = Number(rawCost) || 0;
          console.log(`  - ${project?.name || pid}: $${cost.toFixed(2)}`);

          return {
            _id: project._id || project.id || pid,
            name: project.name || '',
            region: project.region?.name || 'Unknown',
            environments: project.environments || [],
            services: project.services || [],
            cost: cost,
            hasCostData: cost > 0
          };
        });

        return {
          name: account.name,
          success: true,
          projects: projectsWithCost
        };
      } catch (error) {
        console.error(`❌ [${account.name}] 错误:`, error.message);
        return {
          name: account.name,
          success: false,
          error: error.message
        };
      }
    }));

    console.log('📤 返回项目结果');
    res.json(results);
  } catch (error) {
    console.error('❌ /api/temp-projects 未捕获异常:', error);
    res.status(500).json({ error: '/api/temp-projects 服务器错误: ' + error.message });
  }
});

/**
 * 验证账号
 */
router.post('/validate-account', async (req, res) => {
  const { accountName, apiToken } = req.body;

  if (!accountName || !apiToken) {
    return res.status(400).json({ error: '账号名称和 API Token 不能为空' });
  }

  try {
    const { user } = await zeaburApi.fetchAccountData(apiToken);

    if (user._id) {
      res.json({
        success: true,
        message: '账号验证成功！',
        userData: user,
        accountName,
        apiToken
      });
    } else {
      res.status(400).json({ error: 'API Token 无效或没有权限' });
    }
  } catch (error) {
    res.status(400).json({ error: 'API Token 验证失败: ' + error.message });
  }
});

/**
 * 获取所有账号（服务器存储 + 环境变量）
 */
router.get('/server-accounts', async (req, res) => {
  const serverAccounts = storage.loadServerAccounts();
  const envAccounts = storage.getEnvAccounts();

  const allAccounts = [...envAccounts, ...serverAccounts];
  console.log(`📋 返回 ${allAccounts.length} 个账号 (环境变量: ${envAccounts.length}, 服务器: ${serverAccounts.length})`);
  res.json(allAccounts);
});

/**
 * 保存账号到服务器
 */
router.post('/server-accounts', async (req, res) => {
  const { accounts } = req.body;

  if (!accounts || !Array.isArray(accounts)) {
    return res.status(400).json({ error: '无效的账号列表' });
  }

  if (storage.saveServerAccounts(accounts)) {
    console.log(`✅ 保存 ${accounts.length} 个账号到服务器`);
    res.json({ success: true, message: '账号已保存到服务器' });
  } else {
    res.status(500).json({ error: '保存失败' });
  }
});

/**
 * 删除服务器账号
 */
router.delete('/server-accounts/:index', async (req, res) => {
  const index = parseInt(req.params.index);
  const accounts = storage.loadServerAccounts();

  if (index >= 0 && index < accounts.length) {
    const removed = accounts.splice(index, 1);
    if (storage.saveServerAccounts(accounts)) {
      console.log(`🗑️ 删除账号: ${removed[0].name}`);
      res.json({ success: true, message: '账号已删除' });
    } else {
      res.status(500).json({ error: '删除失败' });
    }
  } else {
    res.status(404).json({ error: '账号不存在' });
  }
});

/**
 * 服务器配置的账号API（兼容旧版本）
 */
router.get('/accounts', async (req, res) => {
  const accounts = storage.loadServerAccounts();
  const data = [];

  for (const account of accounts) {
    try {
      const { user, projects, aihub, serviceCosts } = await zeaburApi.fetchAccountData(account.token);

      let usageData = { totalUsage: 0, freeQuotaRemaining: 5, freeQuotaLimit: 5 };
      if (user._id) {
        try {
          usageData = await zeaburApi.fetchUsageData(account.token, user._id, projects);
        } catch (e) {
          console.log(`⚠️ [${account.name}] 获取用量失败:`, e.message);
        }
      }

      const creditInCents = Math.round(usageData.freeQuotaRemaining * 100);
      const totalCost = usageData.totalUsage || 0;

      data.push({
        name: account.name,
        success: true,
        data: {
          ...user,
          credit: creditInCents,
          totalUsage: usageData.totalUsage,
          totalCost: totalCost,
          freeQuotaLimit: usageData.freeQuotaLimit
        },
        aihub: aihub
      });
    } catch (error) {
      console.error(`❌ [${account.name}] 错误:`, error.message);
      data.push({
        name: account.name,
        success: false,
        error: error.message
      });
    }
  }

  res.json(data);
});

/**
 * 获取项目列表
 */
router.get('/projects', async (req, res) => {
  try {
    const serverAccounts = storage.loadServerAccounts();
    const results = await Promise.all(serverAccounts.map(async (account) => {
      try {
        const { user, projects } = await zeaburApi.fetchAccountData(account.token);

        let projectCosts = {};
        if (user._id) {
          try {
            const usageData = await zeaburApi.fetchUsageData(account.token, user._id, projects);
            projectCosts = usageData.projectCosts;
          } catch (e) {
            console.log(`⚠️ [${account.name}] 获取用量失败:`, e.message);
          }
        }

        const projectsWithCost = projects.map(project => {
          const pid = project && (project._id || project.id || (project._id && project._id.$oid)) || '';
          let rawCost = 0;
          if (pid && projectCosts[pid] !== undefined) rawCost = projectCosts[pid];
          else if (project && projectCosts[project.id] !== undefined) rawCost = projectCosts[project.id];
          else rawCost = 0;

          const cost = Number(rawCost) || 0;

          return {
            _id: project._id || project.id || pid,
            name: project.name || '',
            region: project.region?.name || 'Unknown',
            environments: project.environments || [],
            services: project.services || [],
            cost: cost,
            hasCostData: cost > 0
          };
        });

        return { name: account.name, success: true, projects: projectsWithCost };
      } catch (error) {
        console.error(`❌ [${account.name}] 错误:`, error.message);
        return { name: account.name, success: false, error: error.message };
      }
    }));

    res.json(results);
  } catch (error) {
    console.error('❌ /api/projects 未捕获异常:', error);
    res.status(500).json({ error: '/api/projects 服务器错误: ' + error.message });
  }
});

/**
 * 暂停服务
 */
router.post('/service/pause', async (req, res) => {
  const { token, serviceId, environmentId } = req.body;

  if (!token || !serviceId || !environmentId) {
    return res.status(400).json({ error: '缺少必要参数' });
  }

  try {
    const mutation = `mutation { suspendService(serviceID: "${serviceId}", environmentID: "${environmentId}") }`;
    const result = await zeaburApi.queryZeabur(token, mutation);

    if (result.data?.suspendService) {
      res.json({ success: true, message: '服务已暂停' });
    } else {
      res.status(400).json({ error: '暂停失败', details: result });
    }
  } catch (error) {
    res.status(500).json({ error: '暂停服务失败: ' + error.message });
  }
});

/**
 * 重启服务
 */
router.post('/service/restart', async (req, res) => {
  const { token, serviceId, environmentId } = req.body;

  if (!token || !serviceId || !environmentId) {
    return res.status(400).json({ error: '缺少必要参数' });
  }

  try {
    const mutation = `mutation { restartService(serviceID: "${serviceId}", environmentID: "${environmentId}") }`;
    const result = await zeaburApi.queryZeabur(token, mutation);

    if (result.data?.restartService) {
      res.json({ success: true, message: '服务已重启' });
    } else {
      res.status(400).json({ error: '重启失败', details: result });
    }
  } catch (error) {
    res.status(500).json({ error: '重启服务失败: ' + error.message });
  }
});

/**
 * 获取服务日志
 */
router.post('/service/logs', async (req, res) => {
  const { token, serviceId, environmentId, projectId, limit = 200 } = req.body;

  if (!token || !serviceId || !environmentId || !projectId) {
    return res.status(400).json({ error: '缺少必要参数' });
  }

  try {
    const query = `
      query {
        runtimeLogs(
          projectID: "${projectId}"
          serviceID: "${serviceId}"
          environmentID: "${environmentId}"
        ) {
          message
          timestamp
        }
      }
    `;

    const result = await zeaburApi.queryZeabur(token, query);

    if (result.data?.runtimeLogs) {
      const sortedLogs = result.data.runtimeLogs.sort((a, b) => {
        return new Date(a.timestamp) - new Date(b.timestamp);
      });

      const logs = sortedLogs.slice(-limit);

      res.json({
        success: true,
        logs,
        count: logs.length,
        totalCount: result.data.runtimeLogs.length
      });
    } else {
      res.status(400).json({ error: '获取日志失败', details: result });
    }
  } catch (error) {
    res.status(500).json({ error: '获取日志失败: ' + error.message });
  }
});

/**
 * 重命名项目
 */
router.post('/project/rename', async (req, res) => {
  const { token, projectId, newName } = req.body;

  console.log(`📝 收到重命名请求: projectId=${projectId}, newName=${newName}`);

  if (!token || !projectId || !newName) {
    return res.status(400).json({ error: '缺少必要参数' });
  }

  try {
    const mutation = `mutation { renameProject(_id: "${projectId}", name: "${newName}") }`;
    console.log(`🔍 发送 GraphQL mutation:`, mutation);

    const result = await zeaburApi.queryZeabur(token, mutation);
    console.log(`📥 API 响应:`, JSON.stringify(result, null, 2));

    if (result.data?.renameProject) {
      console.log(`✅ 项目已重命名: ${newName}`);
      res.json({ success: true, message: '项目已重命名' });
    } else {
      console.log(`❌ 重命名失败:`, result);
      res.status(400).json({ error: '重命名失败', details: result });
    }
  } catch (error) {
    console.log(`❌ 异常:`, error);
    res.status(500).json({ error: '重命名项目失败: ' + error.message });
  }
});

module.exports = router;
