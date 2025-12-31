require('dotenv').config();
const express = require('express');
const path = require('path');
const fs = require('fs');
const http = require('http');

// 打印 Logo
console.log(`\x1b[36m
  ______   _______   ______         ______    ______         __ 
 /      \\ /       \\ /      |       /      \\  /      \\       /  |
/$$$$$$  |$$$$$$$  |$$$$$$/       /$$$$$$  |/$$$$$$  |      $$ |
$$ |__$$ |$$ |__$$ |  $$ |        $$ | _$$/ $$ |  $$ |      $$ |
$$    $$ |$$    $$/   $$ |        $$ |/    |$$ |  $$ |      $$ |
$$$$$$$$ |$$$$$$$/    $$ |        $$ |$$$$ |$$ |  $$ |      $$/ 
$$ |  $$ |$$ |       _$$ |_       $$ \\__$$ |$$ \\__$$ |       __ 
$$ |  $$ |$$ |      / $$   |      $$    $$/ $$    $$/       /  |
$$/   $$/ $$/       $$$$$$/        $$$$$$/   $$$$$$/        $$/ 
\x1b[0m\x1b[33m
 >>> Gravity Engineering System v0.1.1 测试版 <<<\x1b[0m
`);
// 导入日志工具
const { createLogger } = require('./src/utils/logger');
const logger = createLogger('Server');

// 导入中间件
const corsMiddleware = require('./src/middleware/cors');
const loggerMiddleware = require('./src/middleware/logger');

// 导入服务
const { loadSessions } = require('./src/services/session');
const {
  loadAdminPassword,
  isPasswordSavedToFile,
  loadServerAccounts,
  getEnvAccounts,
} = require('./src/services/config');

// 导入路由
const { registerRoutes } = require('./src/routes');

// 导入日志服务
const logService = require('./src/services/log-service');
// 导入 Metrics 服务
const metricsService = require('./src/services/metrics-service');

const app = express();
// 信任代理 (支持 Zeabur/Cloudflare 等反代获取正确的协议和 IP)
app.set('trust proxy', true);

const server = http.createServer(app);
const PORT = process.env.PORT || 3000;

// 初始化 WebSocket 服务
const logWss = logService.init(server);
const metricsWss = metricsService.init(server);
const sshService = require('./modules/server-management/ssh-service');
const sshWss = sshService.init(server);

// 初始化 Agent Socket.IO 服务 (Nezha 风格实时连接)
const agentService = require('./modules/server-management/agent-service');
agentService.initSocketIO(server);

// 统一处理 WebSocket 升级请求
// 注意: Socket.IO 会自动处理 /socket.io/ 路径的升级请求，这里只处理其他 WebSocket 路径
server.on('upgrade', (request, socket, head) => {
  const pathname = request.url.split('?')[0];

  // Socket.IO 或 Vite HMR 自动处理其命名空间的升级请求，这里直接跳过
  // 增加对 /metrics 和 /agent 的放行，因为它们可能是 Socket.IO 的入口路径
  if (
    pathname.startsWith('/socket.io') ||
    pathname.includes('socket.io') ||
    pathname === '/' ||
    pathname === '/metrics' ||
    pathname === '/agent'
  ) {
    return;
  }

  logger.info(`[WS Upgrade] 路径: ${pathname} (来自 ${socket.remoteAddress})`);

  if (pathname === '/ws/logs') {
    logWss.handleUpgrade(request, socket, head, ws => {
      logger.info('[WS Upgrade] 日志 握手完成');
      logWss.emit('connection', ws, request);
    });
  } else if (pathname === '/ws/metrics') {
    metricsWss.handleUpgrade(request, socket, head, ws => {
      logger.info('[WS Upgrade] Metrics 握手完成');
      metricsWss.emit('connection', ws, request);
    });
  } else if (pathname === '/ws/ssh') {
    sshWss.handleUpgrade(request, socket, head, ws => {
      logger.info('[WS Upgrade] SSH 握手完成');
      sshWss.emit('connection', ws, request);
    });
  } else {
    // 仅针对明确属于 /ws/ 但未识别的路径进行拦截，其他路径交给系统默认处理（或超时断开）
    if (pathname.startsWith('/ws/')) {
      logger.warn(`[WS Upgrade] 拦截未知路径: ${pathname}`);
      socket.destroy();
    }
  }
});

// 初始化日志配置 - 从数据库加载日志文件大小设置
try {
  const { SystemConfig } = require('./src/db/models');
  const { updateLogConfig } = require('./src/utils/logger');
  const savedLogFileSizeMB =
    parseInt(SystemConfig.getConfigValue('log_file_max_size_mb', 10)) || 10;
  updateLogConfig({ maxFileSizeMB: savedLogFileSizeMB });
  logger.info(`日志文件配置已加载: 最大 ${savedLogFileSizeMB} MB`);
} catch (err) {
  logger.warn('加载日志配置失败，使用默认值 10 MB:', err.message);
}

// 应用中间件
app.use(loggerMiddleware);
app.use(corsMiddleware);
app.use(express.json({ limit: '50mb' }));
// 静态文件服务
// 1. 优先服务 dist (生产构建内容)
if (fs.existsSync(path.join(__dirname, 'dist'))) {
  app.use(express.static('dist'));
}

// 2. 总是服务 public 和 src (开发模式资源，或作为生产环境下的动态资源补充，如 Agent 二进制)
app.use(express.static('public'));
app.use(express.static('src'));

// 文件上传中间件
const fileUpload = require('express-fileupload');
app.use(
  fileUpload({
    limits: { fileSize: 100 * 1024 * 1024 }, // 100MB 限制
    abortOnLimit: true,
    createParentPath: true,
  })
);

// Agent 二进制文件静态服务
// 开发模式: public/agent, 生产模式: dist/agent
const agentDir = fs.existsSync(path.join(__dirname, 'dist', 'agent'))
  ? path.join(__dirname, 'dist', 'agent')
  : path.join(__dirname, 'public', 'agent');
if (fs.existsSync(agentDir)) {
  app.use('/agent', express.static(agentDir));
}

// 注册所有路由
// Fly.io module integrated - v4
registerRoutes(app);

// ==================== SPA Fallback 路由 ====================
// 处理前端路由，返回 index.html 让前端路由器处理
// 路径直接使用 mainActiveTab 值
const spaRoutes = [
  '/openai',
  '/antigravity',
  '/gemini-cli',
  '/paas',
  '/dns',
  '/self-h',
  '/server',
  '/totp',
];
spaRoutes.forEach(route => {
  app.get(route, (req, res) => {
    const indexPath = fs.existsSync(path.join(__dirname, 'dist', 'index.html'))
      ? path.join(__dirname, 'dist', 'index.html')
      : path.join(__dirname, 'src', 'index.html');
    res.sendFile(indexPath);
  });
});

// 通用 SPA Fallback：处理所有非 API、非静态文件的 GET 请求
// 确保即使某些路径遗漏也能正确返回 index.html
app.get('*', (req, res, next) => {
  // 跳过 API 和特殊路径
  if (
    req.path.startsWith('/api') ||
    req.path.startsWith('/v1') ||
    req.path.startsWith('/ws') ||
    req.path.startsWith('/health') ||
    req.path.startsWith('/socket.io') ||
    req.path.startsWith('/agent')
  ) {
    return next();
  }

  // 跳过静态资源请求 (带非 .html 扩展名的通常是静态文件)
  const ext = path.extname(req.path);
  if (ext && ext !== '.html') {
    return next();
  }

  // 返回 index.html，让前端路由处理
  const indexPath = fs.existsSync(path.join(__dirname, 'dist', 'index.html'))
    ? path.join(__dirname, 'dist', 'index.html')
    : path.join(__dirname, 'src', 'index.html');
  res.sendFile(indexPath);
});

// 调试路由：捕获异常的 POST /accounts 请求
app.post('/accounts', (req, res) => {
  logger.error('捕获到可疑的 POST /accounts 请求！');
  logger.error('Headers: ' + JSON.stringify(req.headers));
  logger.error('Body: ' + JSON.stringify(req.body));
  res.status(404).json({ error: 'Route not found at root, please use /api/openlist/accounts' });
});

logger.success('所有系统路由及功能模块已就绪 (v4)');

// Favicon 处理
// Favicon 处理 - 前端构建已包含 hash URL，服务端直接返回 204
app.get('/favicon.ico', (req, res) => {
  return res.sendStatus(204);
});

// Logo 处理 - 为生产环境提供 logo.svg
app.get('/logo.svg', (req, res) => {
  const logoPath = path.join(__dirname, 'src', 'logo.svg');
  if (fs.existsSync(logoPath)) {
    return res.sendFile(logoPath);
  }
  return res.sendStatus(404);
});

// 加载持久化 session
loadSessions();

// 启动主机
server.listen(PORT, '0.0.0.0', () => {
  logger.success(`主机启动成功 - http://0.0.0.0:${PORT}`);

  // 检查密码配置
  if (process.env.ADMIN_PASSWORD) {
    logger.info('管理员密码: 环境变量');
  } else if (isPasswordSavedToFile()) {
    logger.info('管理员密码: 文件存储');
  } else {
    logger.warn('未设置管理员密码，首次访问时需设置');
  }

  // 显示数据库统计信息
  try {
    const dbService = require('./src/db/database');
    const stats = dbService.getStats();

    // 计算总数据量
    const zeaburAccounts = stats.tables.zeabur_accounts || 0;
    const zeaburProjects = stats.tables.zeabur_projects || 0;
    const cfAccounts = stats.tables.cf_accounts || 0;
    const cfZones = stats.tables.cf_zones || 0;
    const cfRecords = stats.tables.cf_dns_records || 0;
    const cfTemplates = stats.tables.cf_dns_templates || 0;
    const openaiEndpoints = stats.tables.openai_endpoints || 0;
    const openaiHistory = stats.tables.openai_health_history || 0;
    const sessions = stats.tables.sessions || 0;
    const operationLogs = stats.tables.operation_logs || 0;

    const hasData = zeaburAccounts > 0 || cfAccounts > 0 || openaiEndpoints > 0;

    if (hasData) {
      logger.info('📊 数据库统计信息:');

      // Zeabur 模块
      if (zeaburAccounts > 0 || zeaburProjects > 0) {
        logger.groupItem(`Zeabur: ${zeaburAccounts} 个账号, ${zeaburProjects} 个项目`);
      }

      // Cloudflare DNS 模块
      if (cfAccounts > 0 || cfZones > 0 || cfRecords > 0 || cfTemplates > 0) {
        logger.groupItem(
          `Cloudflare DNS: ${cfAccounts} 个账号, ${cfZones} 个域名, ${cfRecords} 条记录, ${cfTemplates} 个模板`
        );
      }

      // OpenAI 模块
      if (openaiEndpoints > 0 || openaiHistory > 0) {
        logger.groupItem(`OpenAI API: ${openaiEndpoints} 个端点, ${openaiHistory} 条健康检查记录`);
      }

      // 主机管理模块
      const serverAccounts = stats.tables.server_accounts || 0;
      const serverLogs = stats.tables.server_monitor_logs || 0;
      if (serverAccounts > 0 || serverLogs > 0) {
        logger.groupItem(`主机管理: ${serverAccounts} 台主机, ${serverLogs} 条监控日志`);
      }

      // 系统数据
      if (sessions > 0 || operationLogs > 0) {
        logger.groupItem(`系统: ${sessions} 个会话, ${operationLogs} 条操作日志`);
      }

      // 数据库大小
      const dbSizeMB = (stats.dbSize / 1024 / 1024).toFixed(2);
      logger.info(`💾 数据库大小: ${dbSizeMB} MB`);
    } else {
      logger.info('📊 数据库已就绪，等待添加数据');
      logger.info('💡 提示: 可通过各模块页面添加账号和配置');
    }
  } catch (error) {
    logger.warn('无法获取数据库统计信息:', error.message);
  }

  // 启动主机监控服务
  try {
    const monitorService = require('./modules/server-management/monitor-service');
    monitorService.start();
  } catch (error) {
    logger.warn('主机监控服务启动失败:', error.message);
  }

  // 启动自动日志清理任务 (每 12 小时执行一次)
  const AUTO_CLEANUP_INTERVAL = 12 * 60 * 60 * 1000;
  setInterval(() => {
    try {
      const dbService = require('./src/db/database');
      const { SystemConfig } = require('./src/db/models');

      const days = parseInt(SystemConfig.getConfigValue('log_retention_days', 0)) || 0;
      const count = parseInt(SystemConfig.getConfigValue('log_max_count', 0)) || 0;
      const dbSizeMB = parseInt(SystemConfig.getConfigValue('log_max_db_size_mb', 0)) || 0;

      if (days > 0 || count > 0 || dbSizeMB > 0) {
        logger.info('执行定时日志清理任务...');
        const result = dbService.enforceLogLimits({ days, count, dbSizeMB });
        if (result.deleted > 0) {
          logger.success(`定时清理完成，移除 ${result.deleted} 条记录`);
        }
      }
    } catch (error) {
      logger.error('定时日志清理任务失败:', error.message);
    }
  }, AUTO_CLEANUP_INTERVAL);
});
