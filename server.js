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
  getEnvAccounts
} = require('./src/services/config');

// 导入路由
const { registerRoutes } = require('./src/routes');

// 导入 SSH 终端服务
const sshTerminalService = require('./modules/server-management/ssh-terminal-service');

// 导入日志服务
const logService = require('./src/services/log-service');

// 导入实时监控服务
const metricsService = require('./modules/server-management/metrics-service');

const app = express();
const server = http.createServer(app);
const PORT = process.env.PORT || 3000;

// 初始化 WebSocket 服务
const sshWss = sshTerminalService.init(server);
const logWss = logService.init(server);
const metricsWss = metricsService.init(server);

// 统一处理 WebSocket 升级请求
server.on('upgrade', (request, socket, head) => {
  const pathname = request.url.split('?')[0];
  logger.info(`[WS Upgrade] 路径: ${pathname} (来自 ${socket.remoteAddress})`);

  if (pathname === '/ws/ssh') {
    sshWss.handleUpgrade(request, socket, head, (ws) => {
      logger.info(`[WS Upgrade] SSH 握手完成`);
      sshWss.emit('connection', ws, request);
    });
  } else if (pathname === '/ws/logs') {
    logWss.handleUpgrade(request, socket, head, (ws) => {
      logger.info(`[WS Upgrade] 日志 握手完成`);
      logWss.emit('connection', ws, request);
    });
  } else if (pathname === '/ws/metrics') {
    metricsWss.handleUpgrade(request, socket, head, (ws) => {
      logger.info(`[WS Upgrade] 监控指标 握手完成`);
      metricsWss.emit('connection', ws, request);
    });
  } else {
    logger.warn(`[WS Upgrade] 拦截未知路径: ${pathname}`);
    socket.destroy();
  }
});

// 初始化日志配置 - 从数据库加载日志文件大小设置
try {
  const { SystemConfig } = require('./src/db/models');
  const { updateLogConfig } = require('./src/utils/logger');
  const savedLogFileSizeMB = parseInt(SystemConfig.getConfigValue('log_file_max_size_mb', 10)) || 10;
  updateLogConfig({ maxFileSizeMB: savedLogFileSizeMB });
  logger.info(`日志文件配置已加载: 最大 ${savedLogFileSizeMB} MB`);
} catch (err) {
  logger.warn('加载日志配置失败，使用默认值 10 MB:', err.message);
}

// 应用中间件
app.use(loggerMiddleware);
app.use(corsMiddleware);
app.use(express.json({ limit: '50mb' }));
// 静态文件服务 - 优先服务 dist (生产构建)，否则 serving src (开发模式)
if (fs.existsSync(path.join(__dirname, 'dist'))) {
  app.use(express.static('dist'));
} else {
  // 按照 Vite 的逻辑，根目录资源在 public，源代码在 src
  app.use(express.static('public'));
  app.use(express.static('src'));
}

// 文件上传中间件
const fileUpload = require('express-fileupload');
app.use(fileUpload({
  limits: { fileSize: 100 * 1024 * 1024 }, // 100MB 限制
  abortOnLimit: true,
  createParentPath: true
}));

// 注册所有路由
// Fly.io module integrated - v4
registerRoutes(app);

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
        logger.groupItem(`Cloudflare DNS: ${cfAccounts} 个账号, ${cfZones} 个域名, ${cfRecords} 条记录, ${cfTemplates} 个模板`);
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

  // 启动历史指标采集器
  try {
    metricsService.startHistoryCollector();
  } catch (error) {
    logger.warn('历史指标采集器启动失败:', error.message);
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
