/**
 * Zeabur API 管理 - API 路由
 */

const express = require('express');
const router = express.Router();
const storage = require('./storage');
const zeaburApi = require('./zeabur-api');
const { createLogger } = require('../../src/utils/logger');

const logger = createLogger('Zeabur');

/**
 * 临时账号API - 获取账号信息
 */
router.post('/temp-accounts', async (req, res) => {
  try {
    const { accounts } = req.body;

    if (!accounts || !Array.isArray(accounts)) {
      return res.status(400).json({ error: '无效的账号列表' });
    }

    logger.info(`获取账号信息 (${accounts.length}个)`);

    const results = await Promise.all(accounts.map(async (account) => {
      try {
        const { user, projects, aihub, serviceCosts } = await zeaburApi.fetchAccountData(account.token);

        let usageData = { totalUsage: 0, freeQuotaRemaining: 5, freeQuotaLimit: 5 };
        if (user._id) {
          try {
            usageData = await zeaburApi.fetchUsageData(account.token, user._id, projects);
            logger.groupItem(`${account.name}: 用量 $${usageData.totalUsage.toFixed(2)}, 剩余 $${usageData.freeQuotaRemaining.toFixed(2)}`);
          } catch (e) {
            logger.warn(`${account.name}: 获取用量失败 - ${e.message}`);
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
        logger.error(`${account.name}: ${error.message}`);
        return {
          name: account.name,
          success: false,
          error: error.message
        };
      }
    }));

    logger.success(`返回 ${results.length} 个账号信息`);
    res.json(results);
  } catch (error) {
    logger.error('获取账号信息失败', error.message);
    res.status(500).json({ error: '主机错误: ' + error.message });
  }
});

/**
 * 临时账号API - 获取项目信息
 */
router.post('/temp-projects', async (req, res) => {
  try {
    const { accounts } = req.body;

    logger.info(`获取项目信息 (${accounts.length}个账号)`);

    if (!accounts || !Array.isArray(accounts)) {
      return res.status(400).json({ error: '无效的账号列表' });
    }

    const results = await Promise.all(accounts.map(async (account) => {
      try {
        
        const { user, projects } = await zeaburApi.fetchAccountData(account.token);

        let projectCosts = {};
        if (user._id) {
          try {
            const usageData = await zeaburApi.fetchUsageData(account.token, user._id, projects);
            projectCosts = usageData.projectCosts;
          } catch (e) {
            logger.warn(`${account.name}: 获取用量失败 - ${e.message}`);
          }
        }

        logger.groupItem(`${account.name}: ${projects.length} 个项目`);

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

        return {
          name: account.name,
          success: true,
          projects: projectsWithCost
        };
      } catch (error) {
        logger.error(`${account.name}: ${error.message}`);
        return {
          name: account.name,
          success: false,
          error: error.message
        };
      }
    }));

    logger.success(`返回 ${results.length} 个账号的项目信息`);
    res.json(results);
  } catch (error) {
    logger.error('获取项目信息失败', error.message);
    res.status(500).json({ error: '/api/temp-projects 主机错误: ' + error.message });
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
    const { user, projects } = await zeaburApi.fetchAccountData(apiToken);

    if (user._id) {
      // 获取用量数据以计算余额
      let usageData = { totalUsage: 0, freeQuotaRemaining: 5, freeQuotaLimit: 5 };
      try {
        usageData = await zeaburApi.fetchUsageData(apiToken, user._id, projects);
      } catch (e) {
        logger.warn(`${accountName}: 获取用量失败 - ${e.message}`);
      }

      const creditInCents = Math.round(usageData.freeQuotaRemaining * 100);

      res.json({
        success: true,
        message: '账号验证成功！',
        userData: {
          ...user,
          credit: creditInCents,
          totalUsage: usageData.totalUsage,
          freeQuotaLimit: usageData.freeQuotaLimit
        },
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
 * 获取所有账号（主机存储 + 环境变量）
 */
router.get('/server-accounts', async (req, res) => {
  const serverAccounts = storage.loadServerAccounts();
  const envAccounts = storage.getEnvAccounts();

  const allAccounts = [...envAccounts, ...serverAccounts];
  logger.info(`加载 ${allAccounts.length} 个账号 (环境: ${envAccounts.length}, 主机: ${serverAccounts.length})`);
  res.json(allAccounts);
});

/**
 * 保存账号到主机
 */
router.post('/server-accounts', async (req, res) => {
  const { accounts } = req.body;

  if (!accounts || !Array.isArray(accounts)) {
    return res.status(400).json({ error: '无效的账号列表' });
  }

  if (storage.saveServerAccounts(accounts)) {
    logger.success(`保存 ${accounts.length} 个账号`);
    res.json({ success: true, message: '账号已保存到主机' });
  } else {
    res.status(500).json({ error: '保存失败' });
  }
});

/**
 * 删除主机账号
 */
router.delete('/server-accounts/:index', async (req, res) => {
  const index = parseInt(req.params.index);
  const accounts = storage.loadServerAccounts();

  if (index >= 0 && index < accounts.length) {
    const removed = accounts.splice(index, 1);
    if (storage.saveServerAccounts(accounts)) {
      logger.info(`删除账号: ${removed[0].name}`);
      res.json({ success: true, message: '账号已删除' });
    } else {
      res.status(500).json({ error: '删除失败' });
    }
  } else {
    res.status(404).json({ error: '账号不存在' });
  }
});

/**
 * 主机配置的账号API（兼容旧版本）
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
          logger.warn(`${account.name}: 获取用量失败 - ${e.message}`);
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
      logger.error(`${account.name}: ${error.message}`);
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
            logger.warn(`${account.name}: 获取用量失败 - ${e.message}`);
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
        logger.error(`${account.name}: ${error.message}`);
        return { name: account.name, success: false, error: error.message };
      }
    }));

    res.json(results);
  } catch (error) {
    logger.error('获取项目失败', error.message);
    res.status(500).json({ error: '/api/projects 主机错误: ' + error.message });
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

      console.log(`📋 获取服务日志: serviceId=${serviceId.slice(0, 8)}..., 返回 ${logs.length}/${result.data.runtimeLogs.length} 条`);

      res.json({
        success: true,
        logs,
        count: logs.length,
        totalCount: result.data.runtimeLogs.length
      });
    } else {
      console.log(`❌ 获取日志失败: serviceId=${serviceId.slice(0, 8)}...`);
      res.status(400).json({ error: '获取日志失败', details: result });
    }
  } catch (error) {
    console.error(`❌ 获取日志异常: ${error.message}`);
    res.status(500).json({ error: '获取日志失败: ' + error.message });
  }
});

/**
 * 重命名项目
 */
router.post('/project/rename', async (req, res) => {
  const { token, projectId, newName } = req.body;

  if (!token || !projectId || !newName) {
    return res.status(400).json({ error: '缺少必要参数' });
  }

  try {
    const mutation = `mutation {
      renameProject(_id: "${projectId}", name: "${newName}")
    }`;
    const result = await zeaburApi.queryZeabur(token, mutation);

    if (result.data?.renameProject !== undefined) {
      logger.success(`项目已重命名: ${projectId.slice(0, 8)}... -> "${newName}"`);
      res.json({ success: true, message: '项目已重命名' });
    } else if (result.errors) {
      logger.error(`重命名项目失败: ${projectId.slice(0, 8)}... -> "${newName}"`, result);
      const errorMsg = result.errors[0]?.message || '重命名失败';
      res.status(400).json({ error: errorMsg, details: result });
    } else {
      logger.error(`重命名项目失败: ${projectId.slice(0, 8)}... -> "${newName}"`);
      res.status(400).json({ error: '重命名失败', details: result });
    }
  } catch (error) {
    logger.error(`重命名项目异常: ${error.message}`);
    res.status(500).json({ error: '重命名项目失败: ' + error.message });
  }
});

/**
 * 删除项目
 */
router.post('/project/delete', async (req, res) => {
  const { token, projectId } = req.body;

  if (!token || !projectId) {
    return res.status(400).json({ error: '缺少必要参数' });
  }

  try {
    const mutation = `mutation { deleteProject(_id: "${projectId}") }`;
    logger.info(`执行删除项目 mutation: ${mutation}`);
    const result = await zeaburApi.queryZeabur(token, mutation);
    logger.info(`删除项目响应:`, JSON.stringify(result, null, 2));

    if (result.data?.deleteProject === true) {
      logger.success(`项目已删除: ${projectId.slice(0, 8)}...`);
      res.json({ success: true, message: '项目已删除' });
    } else if (result.errors) {
      logger.error(`删除项目失败: ${projectId.slice(0, 8)}...`, result);
      const errorMsg = result.errors[0]?.message || '删除失败';
      res.status(400).json({ error: errorMsg, details: result });
    } else {
      logger.error(`删除项目失败: ${projectId.slice(0, 8)}...`, result);
      res.status(400).json({ error: '删除失败', details: result });
    }
  } catch (error) {
    logger.error(`删除项目异常: ${error.message}`);
    res.status(500).json({ error: '删除项目失败: ' + error.message });
  }
});

/**
 * 删除服务
 */
router.post('/service/delete', async (req, res) => {
  const { token, serviceId } = req.body;

  if (!token || !serviceId) {
    return res.status(400).json({ error: '缺少必要参数' });
  }

  try {
    const mutation = `mutation { deleteService(_id: "${serviceId}") }`;
    logger.info(`执行删除服务 mutation: ${mutation}`);
    const result = await zeaburApi.queryZeabur(token, mutation);
    logger.info(`删除服务响应:`, JSON.stringify(result, null, 2));

    if (result.data?.deleteService === true) {
      logger.success(`服务已删除: ${serviceId.slice(0, 8)}...`);
      res.json({ success: true, message: '服务已删除' });
    } else if (result.errors) {
      logger.error(`删除服务失败: ${serviceId.slice(0, 8)}...`, result);
      const errorMsg = result.errors[0]?.message || '删除失败';
      res.status(400).json({ error: errorMsg, details: result });
    } else {
      logger.error(`删除服务失败: ${serviceId.slice(0, 8)}...`, result);
      res.status(400).json({ error: '删除失败', details: result });
    }
  } catch (error) {
    logger.error(`删除服务异常: ${error.message}`);
    res.status(500).json({ error: '删除服务失败: ' + error.message });
  }
});

/**
 * 重命名服务
 */
router.post('/service/rename', async (req, res) => {
  const { token, serviceId, newName } = req.body;

  if (!token || !serviceId || !newName) {
    return res.status(400).json({ error: '缺少必要参数' });
  }

  try {
    const mutation = `mutation {
      renameService(_id: "${serviceId}", name: "${newName}")
    }`;
    const result = await zeaburApi.queryZeabur(token, mutation);

    if (result.data?.renameService !== undefined) {
      logger.success(`服务已重命名: ${serviceId.slice(0, 8)}... -> "${newName}"`);
      res.json({ success: true, message: '服务已重命名' });
    } else if (result.errors) {
      logger.error(`重命名服务失败: ${serviceId.slice(0, 8)}... -> "${newName}"`, result);
      const errorMsg = result.errors[0]?.message || '重命名失败';
      res.status(400).json({ error: errorMsg, details: result });
    } else {
      logger.error(`重命名服务失败: ${serviceId.slice(0, 8)}... -> "${newName}"`);
      res.status(400).json({ error: '重命名失败', details: result });
    }
  } catch (error) {
    logger.error(`重命名服务异常: ${error.message}`);
    res.status(500).json({ error: '重命名服务失败: ' + error.message });
  }
});

/**
 * 生成免费 Zeabur 域名
 */
router.post('/domain/generate', async (req, res) => {
  const { token, serviceId } = req.body;

  if (!token || !serviceId) {
    return res.status(400).json({ error: '缺少必要参数' });
  }

  try {
    const mutation = `mutation {
      generateDomain(serviceID: "${serviceId}") {
        domain
        status
      }
    }`;

    logger.info(`生成域名: serviceId=${serviceId.slice(0, 8)}...`);
    const result = await zeaburApi.queryZeabur(token, mutation);

    if (result.data?.generateDomain) {
      logger.success(`域名已生成: ${result.data.generateDomain.domain}`);
      res.json({
        success: true,
        message: '域名已生成',
        domain: result.data.generateDomain
      });
    } else if (result.errors) {
      logger.error(`生成域名失败: ${serviceId.slice(0, 8)}...`, result);
      const errorMsg = result.errors[0]?.message || '生成失败';
      res.status(400).json({ error: errorMsg, details: result });
    } else {
      logger.error(`生成域名失败: ${serviceId.slice(0, 8)}...`);
      res.status(400).json({ error: '生成失败', details: result });
    }
  } catch (error) {
    logger.error(`生成域名异常: ${error.message}`);
    res.status(500).json({ error: '生成域名失败: ' + error.message });
  }
});

/**
 * 添加自定义域名
 */
router.post('/domain/add', async (req, res) => {
  const { token, serviceId, domain } = req.body;

  if (!token || !serviceId || !domain) {
    return res.status(400).json({ error: '缺少必要参数' });
  }

  try {
    const mutation = `mutation {
      addCustomDomain(serviceID: "${serviceId}", domain: "${domain}") {
        domain
        status
        dnsRecord {
          type
          name
          value
        }
      }
    }`;

    logger.info(`添加自定义域名: ${domain} -> serviceId=${serviceId.slice(0, 8)}...`);
    const result = await zeaburApi.queryZeabur(token, mutation);

    if (result.data?.addCustomDomain) {
      logger.success(`自定义域名已添加: ${domain}`);
      res.json({
        success: true,
        message: '自定义域名已添加',
        domainInfo: result.data.addCustomDomain
      });
    } else if (result.errors) {
      logger.error(`添加自定义域名失败: ${domain}`, result);
      const errorMsg = result.errors[0]?.message || '添加失败';
      res.status(400).json({ error: errorMsg, details: result });
    } else {
      logger.error(`添加自定义域名失败: ${domain}`);
      res.status(400).json({ error: '添加失败', details: result });
    }
  } catch (error) {
    logger.error(`添加自定义域名异常: ${error.message}`);
    res.status(500).json({ error: '添加自定义域名失败: ' + error.message });
  }
});

/**
 * 删除域名
 */
router.post('/domain/delete', async (req, res) => {
  const { token, serviceId, domain } = req.body;

  if (!token || !serviceId || !domain) {
    return res.status(400).json({ error: '缺少必要参数' });
  }

  try {
    const mutation = `mutation {
      removeDomain(serviceID: "${serviceId}", domain: "${domain}")
    }`;

    logger.info(`删除域名: ${domain} (serviceId=${serviceId.slice(0, 8)}...)`);
    const result = await zeaburApi.queryZeabur(token, mutation);

    if (result.data?.removeDomain !== undefined) {
      logger.success(`域名已删除: ${domain}`);
      res.json({ success: true, message: '域名已删除' });
    } else if (result.errors) {
      logger.error(`删除域名失败: ${domain}`, result);
      const errorMsg = result.errors[0]?.message || '删除失败';
      res.status(400).json({ error: errorMsg, details: result });
    } else {
      logger.error(`删除域名失败: ${domain}`);
      res.status(400).json({ error: '删除失败', details: result });
    }
  } catch (error) {
    logger.error(`删除域名异常: ${error.message}`);
    res.status(500).json({ error: '删除域名失败: ' + error.message });
  }
});

module.exports = router;
