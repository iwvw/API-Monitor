require('dotenv').config();
const express = require('express');
const cors = require('cors');
const https = require('https');
const fs = require('fs');
const path = require('path');

const app = express();
const PORT = process.env.PORT || 3000;

app.use(cors());
app.use(express.json());

// -----------------------------
// 会话机制（内存存储）
// - session 存储在服务器内存，服务器重启后清空
// - session 有效期：2 天
// - 会话通过 HttpOnly cookie `sid` 识别
// - 兼容旧的 x-admin-password header（用于脚本），但优先使用 session
// -----------------------------

const crypto = require('crypto');

// sessionId -> { expires: timestamp }
const sessions = Object.create(null);
const SESSION_TTL_MS = 2 * 24 * 60 * 60 * 1000; // 2 天

function cleanupSessions() {
  const now = Date.now();
  for (const sid of Object.keys(sessions)) {
    if (sessions[sid].expires <= now) {
      delete sessions[sid];
    }
  }
}

// 定期清理过期会话（每小时）
setInterval(cleanupSessions, 60 * 60 * 1000);

function parseCookies(req) {
  const header = req.headers && req.headers.cookie;
  const result = Object.create(null);
  if (!header) return result;
  header.split(';').forEach(pair => {
    const idx = pair.indexOf('=');
    if (idx < 0) return;
    const key = pair.slice(0, idx).trim();
    const val = pair.slice(idx + 1).trim();
    result[key] = decodeURIComponent(val);
  });
  return result;
}

function createSession() {
  const sid = crypto.randomBytes(24).toString('hex');
  const now = Date.now();
  sessions[sid] = { created: now, expires: now + SESSION_TTL_MS };
  return sid;
}

function getSession(req) {
  const cookies = parseCookies(req);
  const sid = cookies.sid;
  if (!sid) return null;
  const session = sessions[sid];
  if (!session) return null;
  if (session.expires <= Date.now()) {
    delete sessions[sid];
    return null;
  }
  // 延长会话到固定 TTL（可选，保持静态过期不延长也可）
  // sessions[sid].expires = Date.now() + SESSION_TTL_MS;
  return { sid, ...session };
}

function destroySession(req) {
  const cookies = parseCookies(req);
  const sid = cookies.sid;
  if (sid && sessions[sid]) delete sessions[sid];
}

// 密码/会话验证中间件
function requireAuth(req, res, next) {
  const session = getSession(req);
  if (session) return next();

  // 回退到旧的 header 验证（保持兼容）
  const password = req.headers['x-admin-password'];
  const savedPassword = loadAdminPassword();

  if (!savedPassword) {
    // 如果没有设置密码，允许访问（首次设置）
    return next();
  }

  if (password === savedPassword) return next();

  return res.status(401).json({ error: '未认证或密码错误' });
}

app.use(express.static('public'));

// 为确保浏览器请求 favicon 时能正确返回图标（兼容 /favicon.ico 请求）
app.get('/favicon.ico', (req, res) => {
  const faviconPath = path.join(__dirname, 'public', 'logo.png');
  if (fs.existsSync(faviconPath)) {
    return res.sendFile(faviconPath);
  }
  return res.sendStatus(204);
});

// 配置目录（可通过环境变量覆盖），优先使用挂载的配置目录
// 推荐在 Docker 中挂载为 `/app/config`，或在本地使用 `./data` 挂载到该路径
const CONFIG_DIR = process.env.CONFIG_DIR || path.join(__dirname, 'config');
// 数据文件路径
const ACCOUNTS_FILE = path.join(CONFIG_DIR, 'accounts.json');
const PASSWORD_FILE = path.join(CONFIG_DIR, 'password.json');

// 读取服务器存储的账号
function loadServerAccounts() {
  try {
    if (fs.existsSync(ACCOUNTS_FILE)) {
      // 检查是否是文件而非目录
      const stats = fs.statSync(ACCOUNTS_FILE);
      if (!stats.isFile()) {
        console.error('❌ accounts.json 是目录而非文件，正在删除...');
        fs.rmSync(ACCOUNTS_FILE, { recursive: true });
        return [];
      }
      const data = fs.readFileSync(ACCOUNTS_FILE, 'utf8');
      return JSON.parse(data);
    }
  } catch (e) {
    console.error('❌ 读取账号文件失败:', e.message);
  }
  return [];
}

// 保存账号到服务器
function saveServerAccounts(accounts) {
  try {
    // 确保配置目录存在
    if (!fs.existsSync(CONFIG_DIR)) {
      fs.mkdirSync(CONFIG_DIR, { recursive: true });
    }

    // 如果目标路径是目录则删除以恢复为文件
    if (fs.existsSync(ACCOUNTS_FILE)) {
      const stats = fs.statSync(ACCOUNTS_FILE);
      if (!stats.isFile()) {
        console.warn('⚠️ 发现 accounts.json 是目录，正在删除...');
        fs.rmSync(ACCOUNTS_FILE, { recursive: true });
      }
    }

    fs.writeFileSync(ACCOUNTS_FILE, JSON.stringify(accounts, null, 2), 'utf8');
    return true;
  } catch (e) {
    console.error('❌ 保存账号文件失败:', e.message);
    return false;
  }
}

// 读取管理员密码（优先环境变量，其次文件）
function loadAdminPassword() {
  // 优先从环境变量读取
  if (process.env.ADMIN_PASSWORD) {
    return process.env.ADMIN_PASSWORD;
  }
  
  // 其次从文件读取
  try {
    if (fs.existsSync(PASSWORD_FILE)) {
      // 检查是否是文件而非目录
      const stats = fs.statSync(PASSWORD_FILE);
      if (!stats.isFile()) {
        console.error('❌ password.json 是目录而非文件，正在删除...');
        fs.rmSync(PASSWORD_FILE, { recursive: true });
        return null;
      }
      const data = fs.readFileSync(PASSWORD_FILE, 'utf8');
      return JSON.parse(data).password;
    }
  } catch (e) {
    console.error('❌ 读取密码文件失败:', e.message);
  }
  return null;
}

// 检查密码是否已在文件中设置（用于 /api/set-password 判断）
function isPasswordSavedToFile() {
  try {
    if (fs.existsSync(PASSWORD_FILE)) {
      const stats = fs.statSync(PASSWORD_FILE);
      if (!stats.isFile()) {
        return false;
      }
      const data = fs.readFileSync(PASSWORD_FILE, 'utf8');
      const parsed = JSON.parse(data);
      return !!parsed.password;
    }
  } catch (e) {
    return false;
  }
  return false;
}

// 保存管理员密码
function saveAdminPassword(password) {
  try {
    // 确保配置目录存在
    if (!fs.existsSync(CONFIG_DIR)) {
      fs.mkdirSync(CONFIG_DIR, { recursive: true });
    }

    // 如果目标路径是目录则删除以恢复为文件
    if (fs.existsSync(PASSWORD_FILE)) {
      const stats = fs.statSync(PASSWORD_FILE);
      if (!stats.isFile()) {
        console.warn('⚠️ 发现 password.json 是目录，正在删除...');
        fs.rmSync(PASSWORD_FILE, { recursive: true });
      }
    }

    fs.writeFileSync(PASSWORD_FILE, JSON.stringify({ password }, null, 2), 'utf8');
    return true;
  } catch (e) {
    console.error('❌ 保存密码文件失败:', e.message);
    return false;
  }
}

// Zeabur GraphQL 查询
async function queryZeabur(token, query) {
  return new Promise((resolve, reject) => {
    const data = JSON.stringify({ query });
    const options = {
      hostname: 'api.zeabur.com',
      path: '/graphql',
      method: 'POST',
      headers: {
        'Authorization': `Bearer ${token}`,
        'Content-Type': 'application/json',
        'Content-Length': data.length
      },
      timeout: 10000
    };

    const req = https.request(options, (res) => {
      let body = '';
      res.on('data', (chunk) => body += chunk);
      res.on('end', () => {
        try {
          resolve(JSON.parse(body));
        } catch (e) {
          reject(new Error('Invalid JSON response'));
        }
      });
    });

    req.on('error', reject);
    req.on('timeout', () => {
      req.destroy();
      reject(new Error('Request timeout'));
    });
    req.write(data);
    req.end();
  });
}

// 获取用户信息和项目
async function fetchAccountData(token) {
  // 查询用户信息
  const userQuery = `
    query {
      me {
        _id
        username
        email
        credit
      }
    }
  `;
  
  // 查询项目信息
  const projectsQuery = `
    query {
      projects {
        edges {
          node {
            _id
            name
            region {
              name
            }
            environments {
              _id
            }
            services {
              _id
              name
              status
              template
              resourceLimit {
                cpu
                memory
              }
              domains {
                domain
                isGenerated
              }
            }
          }
        }
      }
    }
  `;
  
  // 查询 AI Hub 余额
  const aihubQuery = `
    query GetAIHubTenant {
      aihubTenant {
        balance
        keys {
          keyID
          alias
          cost
        }
      }
    }
  `;
  
  const [userData, projectsData, aihubData] = await Promise.all([
    queryZeabur(token, userQuery),
    queryZeabur(token, projectsQuery),
    queryZeabur(token, aihubQuery).catch(() => ({ data: { aihubTenant: null } }))
  ]);
  
  return {
    user: userData.data?.me || {},
    projects: (projectsData.data?.projects?.edges || []).map(edge => edge.node),
    aihub: aihubData.data?.aihubTenant || null
  };
}

// 获取项目用量数据
async function fetchUsageData(token, userID, projects = []) {
  const now = new Date();
  const year = now.getFullYear();
  const month = now.getMonth() + 1;
  const fromDate = `${year}-${String(month).padStart(2, '0')}-01`;
  // 使用明天的日期确保包含今天的所有数据
  const tomorrow = new Date(now);
  tomorrow.setDate(tomorrow.getDate() + 1);
  const toDate = `${tomorrow.getFullYear()}-${String(tomorrow.getMonth() + 1).padStart(2, '0')}-${String(tomorrow.getDate()).padStart(2, '0')}`;
  
  const usageQuery = {
    operationName: 'GetHeaderMonthlyUsage',
    variables: {
      from: fromDate,
      to: toDate,
      groupByEntity: 'PROJECT',
      groupByTime: 'DAY',
      groupByType: 'ALL',
      userID: userID
    },
    query: `query GetHeaderMonthlyUsage($from: String!, $to: String!, $groupByEntity: GroupByEntity, $groupByTime: GroupByTime, $groupByType: GroupByType, $userID: ObjectID!) {
      usages(
        from: $from
        to: $to
        groupByEntity: $groupByEntity
        groupByTime: $groupByTime
        groupByType: $groupByType
        userID: $userID
      ) {
        categories
        data {
          id
          name
          groupByEntity
          usageOfEntity
          __typename
        }
        __typename
      }
    }`
  };
  
  return new Promise((resolve, reject) => {
    const data = JSON.stringify(usageQuery);
    const options = {
      hostname: 'api.zeabur.com',
      path: '/graphql',
      method: 'POST',
      headers: {
        'Authorization': `Bearer ${token}`,
        'Content-Type': 'application/json',
        'Content-Length': Buffer.byteLength(data)
      },
      timeout: 10000
    };

    const req = https.request(options, (res) => {
      let body = '';
      res.on('data', (chunk) => body += chunk);
      res.on('end', () => {
        try {
          const result = JSON.parse(body);
          const usages = result.data?.usages?.data || [];
          
          // 计算每个项目的总费用
          const projectCosts = {};
          let totalUsage = 0;
          
          usages.forEach(project => {
            const projectTotal = project.usageOfEntity.reduce((a, b) => a + b, 0);
            // 单个项目显示：向上取整到 $0.01（与 Zeabur 官方一致）
            const displayCost = projectTotal > 0 ? Math.ceil(projectTotal * 100) / 100 : 0;
            projectCosts[project.id] = displayCost;
            // 总用量计算：使用原始费用（不取整，保证总余额准确）
            totalUsage += projectTotal;
          });
          
          resolve({
            projectCosts,
            totalUsage,
            freeQuotaRemaining: 5 - totalUsage, // 免费额度 $5
            freeQuotaLimit: 5
          });
        } catch (e) {
          reject(new Error('Invalid JSON response'));
        }
      });
    });

    req.on('error', reject);
    req.on('timeout', () => {
      req.destroy();
      reject(new Error('Request timeout'));
    });
    req.write(data);
    req.end();
  });
}

// 临时账号API - 获取账号信息
app.post('/api/temp-accounts', requireAuth, express.json(), async (req, res) => {
  const { accounts } = req.body;
  
  console.log('📥 收到账号请求:', accounts?.length, '个账号');
  
  if (!accounts || !Array.isArray(accounts)) {
    return res.status(400).json({ error: '无效的账号列表' });
  }
  
  const results = await Promise.all(accounts.map(async (account) => {
    try {
      console.log(`🔍 正在获取账号 [${account.name}] 的数据...`);
      const { user, projects, aihub } = await fetchAccountData(account.token);
      console.log(`   API 返回的 credit: ${user.credit}`);
      
      // 获取用量数据
      let usageData = { totalUsage: 0, freeQuotaRemaining: 5, freeQuotaLimit: 5 };
      if (user._id) {
        try {
          usageData = await fetchUsageData(account.token, user._id, projects);
          console.log(`💰 [${account.name}] 用量: $${usageData.totalUsage.toFixed(2)}, 剩余: $${usageData.freeQuotaRemaining.toFixed(2)}`);
        } catch (e) {
          console.log(`⚠️ [${account.name}] 获取用量失败:`, e.message);
        }
      }
      
      // 计算剩余额度并转换为 credit（以分为单位）
      const creditInCents = Math.round(usageData.freeQuotaRemaining * 100);
      
      return {
        name: account.name,
        success: true,
        data: {
          ...user,
          credit: creditInCents, // 使用计算的剩余额度
          totalUsage: usageData.totalUsage,
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
});

// 临时账号API - 获取项目信息
app.post('/api/temp-projects', requireAuth, express.json(), async (req, res) => {
  const { accounts } = req.body;
  
  console.log('📥 收到项目请求:', accounts?.length, '个账号');
  
  if (!accounts || !Array.isArray(accounts)) {
    return res.status(400).json({ error: '无效的账号列表' });
  }
  
  const results = await Promise.all(accounts.map(async (account) => {
    try {
      console.log(`🔍 正在获取账号 [${account.name}] 的项目...`);
      const { user, projects } = await fetchAccountData(account.token);
      
      // 获取用量数据
      let projectCosts = {};
      if (user._id) {
        try {
          const usageData = await fetchUsageData(account.token, user._id, projects);
          projectCosts = usageData.projectCosts;
        } catch (e) {
          console.log(`⚠️ [${account.name}] 获取用量失败:`, e.message);
        }
      }
      
      console.log(`📦 [${account.name}] 找到 ${projects.length} 个项目`);
      
      const projectsWithCost = projects.map(project => {
        const cost = projectCosts[project._id] || 0;
        console.log(`  - ${project.name}: $${cost.toFixed(2)}`);
        
        return {
          _id: project._id,
          name: project.name,
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
});

// 验证账号
app.post('/api/validate-account', requireAuth, express.json(), async (req, res) => {
  const { accountName, apiToken } = req.body;
  
  if (!accountName || !apiToken) {
    return res.status(400).json({ error: '账号名称和 API Token 不能为空' });
  }
  
  try {
    const { user } = await fetchAccountData(apiToken);
    
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

// 从环境变量读取预配置的账号
function getEnvAccounts() {
  const accountsEnv = process.env.ACCOUNTS;
  if (!accountsEnv) return [];
  
  try {
    // 格式: "账号1名称:token1,账号2名称:token2"
    return accountsEnv.split(',').map(item => {
      const [name, token] = item.split(':');
      return { name: name.trim(), token: token.trim() };
    }).filter(acc => acc.name && acc.token);
  } catch (e) {
    console.error('❌ 解析环境变量 ACCOUNTS 失败:', e.message);
    return [];
  }
}

// 检查是否已设置密码
app.get('/api/check-password', (req, res) => {
  const savedPassword = loadAdminPassword();
  res.json({ hasPassword: !!savedPassword });
});

// 登录：创建 session（使用密码）
app.post('/api/login', express.json(), (req, res) => {
  const { password } = req.body;
  const savedPassword = loadAdminPassword();

  // 如果没有设置密码，不能登录（应先设置）
  if (!savedPassword) return res.status(400).json({ success: false, error: '请先设置管理员密码' });

  if (password !== savedPassword) return res.status(401).json({ success: false, error: '密码错误' });

  const sid = createSession();
  const cookieOptions = {
    httpOnly: true,
    sameSite: 'lax',
    maxAge: SESSION_TTL_MS,
    path: '/'
  };
  if (process.env.NODE_ENV === 'production') cookieOptions.secure = true;

  res.cookie('sid', sid, cookieOptions);
  res.json({ success: true });
});

// 登出：销毁 session
app.post('/api/logout', (req, res) => {
  destroySession(req);
  // 清空 cookie
  res.cookie('sid', '', { httpOnly: true, maxAge: 0, path: '/' });
  res.json({ success: true });
});

// 会话检查
app.get('/api/session', (req, res) => {
  const session = getSession(req);
  res.json({ authenticated: !!session });
});

// 设置管理员密码（首次）
// 如果使用了 ADMIN_PASSWORD 环境变量，则跳过此步骤
app.post('/api/set-password', (req, res) => {
  const { password } = req.body;
  
  // 如果已设置了环境变量密码，拒绝再次设置
  if (process.env.ADMIN_PASSWORD) {
    return res.status(400).json({ error: '密码已通过环境变量设置，无法修改' });
  }
  
  // 检查文件中是否已设置密码
  if (isPasswordSavedToFile()) {
    return res.status(400).json({ error: '密码已设置，无法重复设置' });
  }
  
  if (!password || password.length < 6) {
    return res.status(400).json({ error: '密码长度至少6位' });
  }
  
  if (saveAdminPassword(password)) {
    console.log('✅ 管理员密码已设置');
    res.json({ success: true });
  } else {
    res.status(500).json({ error: '保存密码失败' });
  }
});

// 验证密码
app.post('/api/verify-password', (req, res) => {
  const { password } = req.body;
  const savedPassword = loadAdminPassword();
  
  if (!savedPassword) {
    return res.status(400).json({ success: false, error: '请先设置密码' });
  }
  
  if (password === savedPassword) {
    res.json({ success: true });
  } else {
    res.status(401).json({ success: false, error: '密码错误' });
  }
});

// 获取所有账号（服务器存储 + 环境变量）
app.get('/api/server-accounts', requireAuth, async (req, res) => {
  const serverAccounts = loadServerAccounts();
  const envAccounts = getEnvAccounts();
  
  // 合并账号，环境变量账号优先
  const allAccounts = [...envAccounts, ...serverAccounts];
  console.log(`📋 返回 ${allAccounts.length} 个账号 (环境变量: ${envAccounts.length}, 服务器: ${serverAccounts.length})`);
  res.json(allAccounts);
});

// 保存账号到服务器
app.post('/api/server-accounts', requireAuth, async (req, res) => {
  const { accounts } = req.body;
  
  if (!accounts || !Array.isArray(accounts)) {
    return res.status(400).json({ error: '无效的账号列表' });
  }
  
  if (saveServerAccounts(accounts)) {
    console.log(`✅ 保存 ${accounts.length} 个账号到服务器`);
    res.json({ success: true, message: '账号已保存到服务器' });
  } else {
    res.status(500).json({ error: '保存失败' });
  }
});

// 删除服务器账号
app.delete('/api/server-accounts/:index', requireAuth, async (req, res) => {
  const index = parseInt(req.params.index);
  const accounts = loadServerAccounts();
  
  if (index >= 0 && index < accounts.length) {
    const removed = accounts.splice(index, 1);
    if (saveServerAccounts(accounts)) {
      console.log(`🗑️ 删除账号: ${removed[0].name}`);
      res.json({ success: true, message: '账号已删除' });
    } else {
      res.status(500).json({ error: '删除失败' });
    }
  } else {
    res.status(404).json({ error: '账号不存在' });
  }
});

// 服务器配置的账号API（兼容旧版本）
app.get('/api/accounts', async (req, res) => {
  res.json([]);
});

app.get('/api/projects', async (req, res) => {
  res.json([]);
});

// 暂停服务
app.post('/api/service/pause', requireAuth, async (req, res) => {
  const { token, serviceId, environmentId } = req.body;
  
  if (!token || !serviceId || !environmentId) {
    return res.status(400).json({ error: '缺少必要参数' });
  }
  
  try {
    const mutation = `mutation { suspendService(serviceID: "${serviceId}", environmentID: "${environmentId}") }`;
    const result = await queryZeabur(token, mutation);
    
    if (result.data?.suspendService) {
      res.json({ success: true, message: '服务已暂停' });
    } else {
      res.status(400).json({ error: '暂停失败', details: result });
    }
  } catch (error) {
    res.status(500).json({ error: '暂停服务失败: ' + error.message });
  }
});

// 重启服务
app.post('/api/service/restart', requireAuth, async (req, res) => {
  const { token, serviceId, environmentId } = req.body;
  
  if (!token || !serviceId || !environmentId) {
    return res.status(400).json({ error: '缺少必要参数' });
  }
  
  try {
    const mutation = `mutation { restartService(serviceID: "${serviceId}", environmentID: "${environmentId}") }`;
    const result = await queryZeabur(token, mutation);
    
    if (result.data?.restartService) {
      res.json({ success: true, message: '服务已重启' });
    } else {
      res.status(400).json({ error: '重启失败', details: result });
    }
  } catch (error) {
    res.status(500).json({ error: '重启服务失败: ' + error.message });
  }
});

// 获取服务日志
app.post('/api/service/logs', requireAuth, express.json(), async (req, res) => {
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
    
    const result = await queryZeabur(token, query);
    
    if (result.data?.runtimeLogs) {
      // 按时间戳排序，最新的在最后
      const sortedLogs = result.data.runtimeLogs.sort((a, b) => {
        return new Date(a.timestamp) - new Date(b.timestamp);
      });
      
      // 获取最后 N 条日志
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

// 重命名项目
app.post('/api/project/rename', requireAuth, async (req, res) => {
  const { token, projectId, newName } = req.body;
  
  console.log(`📝 收到重命名请求: projectId=${projectId}, newName=${newName}`);
  
  if (!token || !projectId || !newName) {
    return res.status(400).json({ error: '缺少必要参数' });
  }
  
  try {
    const mutation = `mutation { renameProject(_id: "${projectId}", name: "${newName}") }`;
    console.log(`🔍 发送 GraphQL mutation:`, mutation);
    
    const result = await queryZeabur(token, mutation);
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

app.listen(PORT, '0.0.0.0', () => {
  console.log(`✨ Zeabur Monitor 运行在 http://0.0.0.0:${PORT}`);
  
  // 检查密码配置
  if (process.env.ADMIN_PASSWORD) {
    console.log(`🔐 已通过环境变量 ADMIN_PASSWORD 设置管理员密码`);
  } else if (isPasswordSavedToFile()) {
    console.log(`🔐 管理员密码已保存到文件`);
  } else {
    console.log(`⚠️ 未设置管理员密码，首次访问时请设置`);
  }
  
  const envAccounts = getEnvAccounts();
  const serverAccounts = loadServerAccounts();
  const totalAccounts = envAccounts.length + serverAccounts.length;
  
  if (totalAccounts > 0) {
    console.log(`📋 已加载 ${totalAccounts} 个账号`);
    if (envAccounts.length > 0) {
      console.log(`   环境变量: ${envAccounts.length} 个`);
      envAccounts.forEach(acc => console.log(`     - ${acc.name}`));
    }
    if (serverAccounts.length > 0) {
      console.log(`   服务器存储: ${serverAccounts.length} 个`);
      serverAccounts.forEach(acc => console.log(`     - ${acc.name}`));
    }
  } else {
    console.log(`📊 准备就绪，等待添加账号...`);
  }
});
