/**
 * Fly.io API 路由模块
 */

const express = require('express');
const router = express.Router();
const storage = require('./storage');
const axios = require('axios');
const { createLogger } = require('../../src/utils/logger');
const logger = createLogger('Fly');

logger.info('Module Loaded');

const FLY_API_URL = 'https://api.fly.io/graphql';
const FLY_MACHINES_URL = 'https://api.machines.dev/v1';

// ============ 辅助函数 ============

/**
 * GraphQL 请求封装
 */
async function flyRequest(query, variables = {}, token) {
  try {
    const response = await axios.post(
      FLY_API_URL,
      {
        query,
        variables,
      },
      {
        headers: {
          Authorization: `Bearer ${token}`,
          'Content-Type': 'application/json',
          'User-Agent': 'API-Monitor/1.0',
        },
        timeout: 30000,
      }
    );

    if (response.data.errors) {
      logger.error('GraphQL Errors:', response.data.errors);
      throw new Error(response.data.errors[0].message);
    }

    return response.data;
  } catch (error) {
    if (error.response) {
      logger.error(`API Error: ${error.response.status}`, error.response.data);
      throw new Error(error.response.data?.errors?.[0]?.message || error.message);
    }
    logger.error(`Network Error: ${error.message}`);
    throw error;
  }
}

/**
 * Machines API 请求封装
 */
async function machineRequest(method, path, token, data = null) {
  try {
    const config = {
      method,
      url: `${FLY_MACHINES_URL}${path}`,
      headers: {
        Authorization: `Bearer ${token}`,
        'Content-Type': 'application/json',
        'User-Agent': 'API-Monitor/1.0',
      },
      timeout: 30000,
    };

    if (data) {
      config.data = data;
    }

    const response = await axios(config);
    return response.data;
  } catch (error) {
    if (error.response) {
      console.error(
        `[Fly.io] Machine API Error [${path}]:`,
        error.response.status,
        error.response.data
      );
      throw new Error(error.response.data?.error || error.message);
    }
    throw error;
  }
}

// ============ 账号管理 API ============

/**
 * 获取所有账号列表 (展示用，隐藏 Token)
 */
router.get('/accounts', async (req, res) => {
  try {
    const accounts = await storage.getAccounts();
    // 隐藏敏感信息
    const safeAccounts = accounts.map(acc => {
      const { api_token, ...rest } = acc;
      return rest;
    });
    res.json({ success: true, data: safeAccounts });
  } catch (error) {
    console.error('获取 Fly.io 账号列表失败:', error);
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * 获取所有账号列表 (导出用，包含 Token)
 */
router.get('/accounts/export', async (req, res) => {
  try {
    const accounts = await storage.getAccounts();
    res.json({ success: true, accounts });
  } catch (error) {
    console.error('导出 Fly.io 账号失败:', error);
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * 添加新账号
 */
router.post('/accounts', async (req, res) => {
  try {
    const { name, api_token } = req.body;

    if (!name || !api_token) {
      return res.status(400).json({ success: false, error: '名称和 API Token 必填' });
    }

    // 验证 Token 有效性并获取用户信息
    const query = `
      query {
        viewer {
          email
        }
        organizations {
          nodes {
            id
            slug
            name
          }
        }
      }
    `;

    const result = await flyRequest(query, {}, api_token);
    if (result.errors) {
      throw new Error(result.errors[0].message);
    }

    const email = result.data.viewer?.email || '';
    let defaultOrg = null;

    if (result.data.organizations?.nodes?.length > 0) {
      defaultOrg = result.data.organizations.nodes[0].id;
    }

    const account = await storage.addAccount({
      name,
      api_token,
      email,
      organization_id: defaultOrg,
    });

    res.json({ success: true, data: account });
  } catch (error) {
    console.error('添加 Fly.io 账号失败:', error);
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * 删除账号
 */
router.delete('/accounts/:id', async (req, res) => {
  try {
    await storage.deleteAccount(req.params.id);
    res.json({ success: true });
  } catch (error) {
    console.error('删除 Fly.io 账号失败:', error);
    res.status(500).json({ success: false, error: error.message });
  }
});

// ============ 代理获取数据 API (Dashboard 用) ============

/**
 * 获取所有账号的 Apps 数据
 */
router.get('/proxy/apps', async (req, res) => {
  try {
    const accounts = await storage.getAccounts();
    const results = [];

    // 并行获取所有账号的数据
    await Promise.all(
      accounts.map(async account => {
        const query = `
        query {
          apps {
            nodes {
              id
              name
              status
              deployed
              hostname
              appUrl
              organization {
                slug
              }
              currentRelease {
                createdAt
                status
              }
              machines {
                nodes {
                  id
                  region
                  state
                }
              }
              certificates {
                nodes {
                  hostname
                  clientStatus
                }
              }
              ipAddresses {
                nodes {
                  address
                  type
                }
              }
            }
          }
        }
      `;

        try {
          const result = await flyRequest(query, {}, account.api_token);
          results.push({
            accountId: account.id,
            accountName: account.name,
            apps: result.data.apps?.nodes || [],
            error: null,
          });
        } catch (error) {
          console.error(`获取账号 ${account.name} 的 Apps 失败:`, error.message);
          results.push({
            accountId: account.id,
            accountName: account.name,
            apps: [],
            error: error.message,
          });
        }
      })
    );

    res.json({ success: true, data: results });
  } catch (error) {
    console.error('获取 Fly.io Apps 数据失败:', error);
    res.status(500).json({ success: false, error: error.message });
  }
});

// ============ 应用管理 API ============

/**
 * 创建新应用
 */
router.post('/apps', async (req, res) => {
  try {
    const { accountId, name } = req.body;

    const account = await storage.getAccount(accountId);
    if (!account) {
      return res.status(404).json({ success: false, error: 'Account not found' });
    }

    const mutation = `
      mutation($input: CreateAppInput!) {
        createApp(input: $input) {
          app {
            id
            name
            status
            hostname
          }
        }
      }
    `;

    const variables = {
      input: {
        name: name || undefined,
        organizationId: account.organization_id,
      },
    };

    const result = await flyRequest(mutation, variables, account.api_token);
    res.json({ success: true, data: result.data.createApp.app });
  } catch (error) {
    console.error('创建 Fly.io 应用失败:', error);
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * 删除应用
 */
router.delete('/apps/:appName', async (req, res) => {
  try {
    const { appName } = req.params;
    const { accountId } = req.body;

    const account = await storage.getAccount(accountId);
    if (!account) {
      return res.status(404).json({ success: false, error: 'Account not found' });
    }

    const mutation = `
      mutation($appId: String!) {
        deleteApp(appId: $appId) {
          organization {
            id
          }
        }
      }
    `;

    await flyRequest(mutation, { appId: appName }, account.api_token);
    res.json({ success: true });
  } catch (error) {
    console.error('删除 Fly.io 应用失败:', error);
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * 重命名应用
 */
router.post('/apps/:appName/rename', async (req, res) => {
  try {
    const { appName } = req.params;
    const { accountId, newName } = req.body;

    const account = await storage.getAccount(accountId);
    if (!account) {
      return res.status(404).json({ success: false, error: 'Account not found' });
    }

    const mutation = `
      mutation($input: UpdateAppInput!) {
        updateApp(input: $input) {
          app {
            id
            name
          }
        }
      }
    `;

    const result = await flyRequest(
      mutation,
      {
        input: {
          appId: appName,
          name: newName,
        },
      },
      account.api_token
    );

    res.json({ success: true, data: result.data?.updateApp?.app });
  } catch (error) {
    console.error('重命名 Fly.io 应用失败:', error);
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * 重新部署应用 (重启所有 Machines)
 */
router.post('/apps/:appName/redeploy', async (req, res) => {
  try {
    const { appName } = req.params;
    const { accountId } = req.body;

    const account = await storage.getAccount(accountId);
    if (!account) {
      return res.status(404).json({ success: false, error: 'Account not found' });
    }

    // 获取所有 machines
    const machines = await machineRequest('GET', `/apps/${appName}/machines`, account.api_token);

    if (!machines || machines.length === 0) {
      return res.json({ success: true, message: 'No running machines found' });
    }

    // 并行重启所有 machines
    const restartPromises = machines.map(m =>
      machineRequest('POST', `/apps/${appName}/machines/${m.id}/restart`, account.api_token).catch(
        err => ({ error: true, id: m.id, message: err.message })
      )
    );

    const results = await Promise.all(restartPromises);
    const errors = results.filter(r => r?.error);

    if (errors.length > 0) {
      console.warn('部分 Machine 重启失败:', errors);
    }

    res.json({
      success: true,
      restarted: machines.length - errors.length,
      failed: errors.length,
    });
  } catch (error) {
    console.error('重新部署 Fly.io 应用失败:', error);
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * 更新应用的所有 Machines 镜像 (实现类似 fly deploy 的更新功能)
 */
router.post('/apps/:appName/update-image', async (req, res) => {
  try {
    const { appName } = req.params;
    const { accountId, image } = req.body;

    if (!image) {
      return res.status(400).json({ success: false, error: 'Image is required' });
    }

    const account = await storage.getAccount(accountId);
    if (!account) {
      return res.status(404).json({ success: false, error: 'Account not found' });
    }

    // 1. 获取所有 machines
    const machines = await machineRequest('GET', `/apps/${appName}/machines`, account.api_token);

    if (!machines || machines.length === 0) {
      return res.json({ success: true, message: 'No machines found' });
    }

    // 2. 遍历更新每个 machine 的镜像
    // Fly.io 推荐 "Get-Modify-Post" 模式以保留其他配置 (env, mounts, guest 等)
    const updatePromises = machines.map(async m => {
      try {
        const currentConfig = m.config;
        const updatedConfig = {
          ...currentConfig,
          image: image,
        };

        return await machineRequest(
          'POST',
          `/apps/${appName}/machines/${m.id}`,
          account.api_token,
          {
            config: updatedConfig,
          }
        );
      } catch (err) {
        return { error: true, id: m.id, message: err.message };
      }
    });

    const results = await Promise.all(updatePromises);
    const errors = results.filter(r => r?.error);

    if (errors.length > 0) {
      console.warn('部分 Machine 镜像更新失败:', errors);
    }

    res.json({
      success: true,
      updated: machines.length - errors.length,
      failed: errors.length,
      errors: errors.length > 0 ? errors : undefined,
    });
  } catch (error) {
    console.error('更新 Fly.io 应用镜像失败:', error);
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * 获取应用的 Machines 列表
 */
router.get('/apps/:appName/machines', async (req, res) => {
  try {
    const { appName } = req.params;
    const { accountId } = req.query;

    const account = await storage.getAccount(accountId);
    if (!account) {
      return res.status(404).json({ success: false, error: 'Account not found' });
    }

    const machines = await machineRequest('GET', `/apps/${appName}/machines`, account.api_token);
    res.json({ success: true, data: machines || [] });
  } catch (error) {
    console.error('获取 Fly.io Machines 失败:', error);
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * 获取应用的系统事件日志
 */
router.get('/apps/:appName/events', async (req, res) => {
  try {
    const { appName } = req.params;
    const { accountId } = req.query;

    const account = await storage.getAccount(accountId);
    if (!account) {
      return res.status(404).json({ success: false, error: 'Account not found' });
    }

    // 使用 GraphQL 获取应用事件
    const query = `
      query($appName: String!) {
        app(name: $appName) {
          releases(last: 20) {
            nodes {
              id
              version
              status
              reason
              createdAt
              user {
                email
              }
            }
          }
        }
      }
    `;

    const result = await flyRequest(query, { appName }, account.api_token);
    const releases = result.data.app?.releases?.nodes || [];

    // 转换为统一的事件格式
    const events = releases.map(r => ({
      timestamp: new Date(r.createdAt).getTime(),
      message: `Release v${r.version} - ${r.status}${r.reason ? ': ' + r.reason : ''}`,
      region: 'global',
    }));

    res.json({ success: true, data: events });
  } catch (error) {
    console.error('获取 Fly.io 应用事件失败:', error);
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * 获取应用配置
 */
router.get('/apps/:appName/config', async (req, res) => {
  try {
    const { appName } = req.params;
    const { accountId } = req.query;

    const account = await storage.getAccount(accountId);
    if (!account) {
      return res.status(404).json({ success: false, error: 'Account not found' });
    }

    // 使用 GraphQL 获取应用配置
    const query = `
      query($appName: String!) {
        app(name: $appName) {
          id
          name
          status
          hostname
          appUrl
          organization {
            slug
            name
          }
          regions {
            code
            name
          }
          currentRelease {
            version
            status
            createdAt
          }
          config {
            definition
          }
          secrets {
            name
            createdAt
          }
        }
      }
    `;

    const result = await flyRequest(query, { appName }, account.api_token);
    res.json({ success: true, data: result.data.app });
  } catch (error) {
    console.error('获取 Fly.io 应用配置失败:', error);
    res.status(500).json({ success: false, error: error.message });
  }
});

/**
 * 获取应用的实时日志 (stdout/stderr)
 */
router.get('/apps/:appName/logs', async (req, res) => {
  try {
    const { appName } = req.params;
    const { accountId } = req.query;

    const account = await storage.getAccount(accountId);
    if (!account) {
      return res.status(404).json({ success: false, error: 'Account not found' });
    }

    // 尝试通过 Machines API 获取最近的日志 (这是目前最可靠的方式)
    // 首先获取机器列表
    const machines = await machineRequest('GET', `/apps/${appName}/machines`, account.api_token);
    
    if (!machines || machines.length === 0) {
      return res.json({ success: true, data: [] });
    }

    // 使用 Fly.io 内部日志 API (去掉 instance 过滤以获取更全的日志)
    console.log(`[Fly.io] 正在从应用 ${appName} 获取容器日志...`);

    const response = await axios({
      method: 'GET',
      url: `https://api.fly.io/api/v1/apps/${appName}/logs`,
      headers: {
        Authorization: `Bearer ${account.api_token}`,
        'Accept': 'application/json, text/plain, */*'
      },
      timeout: 7000,
      responseType: 'text' // 关键：必须作为文本读取，因为返回的是 NDJSON
    }).catch(err => {
      console.error('[Fly.io] 请求容器日志 API 异常:', err.message, err.response?.data);
      return { data: '' };
    });

    let logs = [];
    if (response.data && typeof response.data === 'string') {
      const lines = response.data.split('\n').filter(l => l.trim());
      console.log(`[Fly.io] 原始返回 ${lines.length} 行数据`);
      
      logs = lines.map(line => {
        try {
          return JSON.parse(line);
        } catch (e) {
          // 如果某一行不是 JSON，则作为普通文本消息处理
          return { message: line, timestamp: new Date().toISOString() };
        }
      });
    }

    // 过滤并排序日志（按时间从新到旧）
    const formattedLogs = logs
      .filter(l => l.message)
      .map(l => ({
        timestamp: l.timestamp || new Date().toISOString(),
        message: l.message,
        level: l.level || 'info',
        instance: l.instance || '',
        region: l.region || ''
      }))
      .sort((a, b) => new Date(b.timestamp) - new Date(a.timestamp));

    res.json({
      success: true,
      data: formattedLogs
    });
  } catch (error) {
    console.error('获取 Fly.io 容器日志失败:', error);
    // 即使失败也返回 200 和空数组，防止前端报错，让前端自动回退到事件日志
    res.json({ success: true, data: [], error: error.message });
  }
});

/**
 * 一键更新账号下所有应用的镜像 (批量 fly deploy)
 */
router.post('/accounts/:accountId/update-all-images', async (req, res) => {
  try {
    const { accountId } = req.params;
    const { image } = req.body; // 通常是 :latest

    const account = await storage.getAccount(accountId);
    if (!account) {
      return res.status(404).json({ success: false, error: 'Account not found' });
    }

    // 1. 获取该账号下的所有应用
    const query = `
      query {
        apps {
          nodes {
            name
          }
        }
      }
    `;
    const appsResult = await flyRequest(query, {}, account.api_token);
    const apps = appsResult.data.apps?.nodes || [];

    if (apps.length === 0) {
      return res.json({ success: true, message: 'No apps found', updated: 0 });
    }

    console.log(`[Fly.io] 开始为账号 ${account.name} 批量更新 ${apps.length} 个应用`);

    // 2. 并行触发所有应用的更新
    const updatePromises = apps.map(async app => {
      try {
        // 先获取每个应用的 machines
        const machines = await machineRequest('GET', `/apps/${app.name}/machines`, account.api_token);
        if (!machines || machines.length === 0) return { app: app.name, skipped: true };

        const machineUpdates = machines.map(m => {
          let targetImage = image || m.config.image;
          
          // 核心修复：如果 image 参数是 "latest"，则智能替换当前镜像的 tag
          if (image === 'latest') {
            const currentImage = m.config.image;
            if (currentImage && currentImage.includes(':')) {
              targetImage = currentImage.split(':')[0] + ':latest';
            } else if (currentImage) {
              targetImage = currentImage + ':latest';
            }
          }

          const updatedConfig = { ...m.config, image: targetImage };
          
          return machineRequest('POST', `/apps/${app.name}/machines/${m.id}`, account.api_token, {
            config: updatedConfig
          });
        });

        await Promise.all(machineUpdates);
        return { app: app.name, success: true };
      } catch (err) {
        return { app: app.name, error: err.message };
      }
    });

    const results = await Promise.all(updatePromises);
    const successful = results.filter(r => r.success).length;
    const failed = results.filter(r => r.error).length;

    res.json({
      success: true,
      total: apps.length,
      updated: successful,
      failed: failed,
      details: results
    });
  } catch (error) {
    console.error('批量更新 Fly.io 应用失败:', error);
    res.status(500).json({ success: false, error: error.message });
  }
});

module.exports = router;
