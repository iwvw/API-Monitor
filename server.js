require('dotenv').config();
const express = require('express');
const path = require('path');
const fs = require('fs');

// 导入日志工具
const { createLogger } = require('./src/utils/logger');
const logger = createLogger('Server');

// 导入中间件
const corsMiddleware = require('./src/middleware/cors');

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

const app = express();
const PORT = process.env.PORT || 3000;

// 应用中间件
app.use(corsMiddleware);
app.use(express.json());
app.use(express.static('public'));

// 文件上传中间件
const fileUpload = require('express-fileupload');
app.use(fileUpload({
  limits: { fileSize: 100 * 1024 * 1024 }, // 100MB 限制
  abortOnLimit: true,
  createParentPath: true
}));

// 注册所有路由
registerRoutes(app);

// Favicon 处理
app.get('/favicon.ico', (req, res) => {
  const faviconPath = path.join(__dirname, 'public', 'logo.png');
  if (fs.existsSync(faviconPath)) {
    return res.sendFile(faviconPath);
  }
  return res.sendStatus(204);
});

// 加载持久化 session
loadSessions();

// 启动服务器
app.listen(PORT, '0.0.0.0', () => {
  logger.success(`服务器启动成功 - http://0.0.0.0:${PORT}`);

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

      // 服务器管理模块
      const serverAccounts = stats.tables.server_accounts || 0;
      const serverLogs = stats.tables.server_monitor_logs || 0;
      if (serverAccounts > 0 || serverLogs > 0) {
        logger.groupItem(`服务器管理: ${serverAccounts} 台服务器, ${serverLogs} 条监控日志`);
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

  // 启动服务器监控服务
  try {
    const monitorService = require('./modules/server-management/monitor-service');
    monitorService.start();
  } catch (error) {
    logger.warn('服务器监控服务启动失败:', error.message);
  }
});
